package upload

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/plugins/transcoder"
	"streamgate/pkg/service"
	"streamgate/pkg/storage"
)

type UploadServer struct {
	config        *config.Config
	logger        *zap.Logger
	kernel        *core.Microkernel
	server        *http.Server
	svc           *service.UploadService
	transcodingSvc *service.TranscodingService
}

func NewUploadServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*UploadServer, error) {
	pg := storage.NewPostgresDB()
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.User, cfg.Database.Password, cfg.Database.Database, cfg.Database.SSLMode)
	poolCfg := storage.PoolConfigFromValues(cfg.Database.MaxConns, cfg.Database.MaxIdleConns, 0, 0)
	if cfg.Database.ConnMaxLifetime != "" {
		if d, err := time.ParseDuration(cfg.Database.ConnMaxLifetime); err == nil {
			poolCfg.ConnMaxLifetime = d
		}
	}
	if err := pg.ConnectWithConfig(dsn, poolCfg); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	objStore, err := createObjectStorage(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create object storage: %w", err)
	}

	uploadObj := objStore

	segStore, ok := objStore.(service.SegmentStorage)
	if !ok {
		logger.Warn("Object storage does not implement SegmentStorage, transcoding disabled")
	}

	svc := service.NewUploadService(pg, uploadObj, cfg.Storage.Bucket, logger)
	if presigner, ok := objStore.(service.PresignedURLer); ok {
		svc.SetPresigner(presigner)
	}
	if cfg.Upload.MaxSize > 0 {
		svc.SetMaxUploadSize(cfg.Upload.MaxSize)
	}
	if cfg.Upload.StorageQuota > 0 {
		svc.SetStorageQuota(cfg.Upload.StorageQuota)
	}

	var transcodingSvc *service.TranscodingService
	var presigner service.PresignedURLer
	if ps, ok := objStore.(service.PresignedURLer); ok {
		presigner = ps
	}
	if segStore != nil {
		transcodingSvc = initTranscodingService(cfg, logger, pg, segStore)
	}
	svc.RegisterAutoTranscodeHook(service.AutoTranscodeHookDeps{
		TranscodingSvc: transcodingSvc,
		Presigner:      presigner,
		Bucket:         cfg.Storage.Bucket,
		Profiles:       cfg.Transcode.Profiles,
	})

	return &UploadServer{
		config:         cfg,
		logger:         logger,
		kernel:         kernel,
		svc:            svc,
		transcodingSvc: transcodingSvc,
	}, nil
}

func initTranscodingService(cfg *config.Config, log *zap.Logger, db storage.DB, objStorage service.SegmentStorage) *service.TranscodingService {
	ffmpegCfg := &transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     30 * time.Minute,
	}
	ft := transcoder.NewFFmpegTranscoder(ffmpegCfg, log.Named("ffmpeg"))
	videoTranscoder := &ffmpegAdapter{ft: ft, log: log.Named("ffmpeg")}

	var transcodingQueue service.TranscodingQueue
	nq, natsErr := storage.NewNATSTranscodingQueue(cfg.NATS.URL, log.Named("nats-queue"))
	if natsErr != nil {
		log.Warn("NATS unavailable, falling back to in-memory transcoding queue", zap.Error(natsErr))
		transcodingQueue = service.NewMemoryTranscodingQueue()
	} else {
		log.Info("Using NATS JetStream transcoding queue", zap.String("url", cfg.NATS.URL))
		_ = nq
	}

	svc := service.NewTranscodingService(db, transcodingQueue,
		service.WithTranscoder(videoTranscoder),
		service.WithStorage(objStorage),
		service.WithLogger(log),
	)
	svc.StartWorker(&zapInfoLogger{log.Named("transcode-worker")})
	return svc
}

func (s *UploadServer) GetService() *service.UploadService {
	return s.svc
}

func (s *UploadServer) Start(ctx context.Context) error {
	handler := NewUploadHandler(s.svc, s.logger, s.kernel)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)
	mux.HandleFunc("/api/v1/upload", handler.UploadHandler)
	mux.HandleFunc("/api/v1/upload/list", handler.ListUploadsHandler)
	mux.HandleFunc("/api/v1/upload/init", handler.InitChunkedUploadHandler)
	mux.HandleFunc("/api/v1/upload/chunk", handler.UploadChunkHandler)
	mux.HandleFunc("/api/v1/upload/complete", handler.CompleteUploadHandler)
	mux.HandleFunc("/api/v1/upload/complete-upload", handler.CompleteUploadWithContentHandler)
	mux.HandleFunc("/api/v1/upload/status", handler.GetUploadStatusHandler)
	mux.HandleFunc("/api/v1/upload/chunks", handler.ChunkStatusesHandler)
	mux.HandleFunc("/api/v1/upload/download-url", handler.DownloadURLHandler)
	mux.HandleFunc("/api/v1/upload/delete", handler.DeleteUploadHandler)
	mux.HandleFunc("/", handler.NotFoundHandler)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Upload server error", zap.Error(err))
		}
	}()

	return nil
}

func (s *UploadServer) Stop(ctx context.Context) error {
	if s.transcodingSvc != nil {
		s.transcodingSvc.StopWorker()
	}
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down upload server", zap.Error(err))
			return err
		}
	}
	return nil
}

func (s *UploadServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("upload server not started")
	}
	if s.svc == nil {
		return fmt.Errorf("upload service not initialized")
	}
	return nil
}

func createObjectStorage(cfg *config.Config, logger *zap.Logger) (service.UploadObjectStorage, error) {
	switch cfg.Storage.Type {
	case "s3":
		s3Cfg := storage.S3Config{
			Region:          cfg.Storage.Region,
			AccessKeyID:     cfg.Storage.AccessKey,
			SecretAccessKey: cfg.Storage.SecretKey,
			Endpoint:        cfg.Storage.Endpoint,
		}
		return storage.NewS3Storage(s3Cfg)
	default:
		minioCfg := storage.MinIOConfig{
			Endpoint:        cfg.Storage.Endpoint,
			AccessKeyID:     cfg.Storage.AccessKey,
			SecretAccessKey: cfg.Storage.SecretKey,
			UseSSL:          cfg.Storage.UseSSL,
		}
		return storage.NewMinIOStorage(minioCfg)
	}
}
