// The transcoder microservice is the ISOLATED media worker (TRANSITION_PLAN.md
// SG-12 step 5). It runs the same Postgres / JetStream / object-storage /
// service.TranscodingService pipeline as the monolith, but holds NO JWT secret
// and NO chain keys: untrusted media parsing never shares a trust domain with
// the wallet/JWT-signing process. The app processes (monolith, api-gateway)
// submit tasks with transcoding.enabled=false; this worker consumes and
// executes them.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/core/logger"
	"github.com/rtcdance/streamgate/pkg/plugins/transcoder"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"

	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

func main() {
	log := logger.NewDevelopmentLogger("streamgate-transcoder")
	defer func() { _ = log.Sync() }()

	log.Info("Starting StreamGate Transcoder Service (isolated media worker)...")

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	cfg.Mode = "microservice"
	cfg.ServiceName = "transcoder"
	if err := cfg.ValidateProduction(log); err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			var fatal []string
			for _, c := range ve.Critical {
				// The empty JWT secret is DELIBERATE here: this process parses
				// media and never mints or verifies a token, so requiring the
				// signing secret would force the very credential this worker is
				// isolated from back into its environment.
				if strings.HasPrefix(c, "auth.jwt_secret is empty") {
					log.Info("config: auth.jwt_secret absent by design (isolated worker never handles JWTs)")
					continue
				}
				fatal = append(fatal, c)
			}
			if len(fatal) > 0 {
				// Criticals are fatal ONLY under the production profile: the
				// demo and durable-acceptance stacks run on published demo
				// values by design, so their criticals become loud warnings.
				if cfg.EffectiveProfile() == config.ProfileProduction {
					log.Fatal("Critical security config validation failed (cannot be bypassed)", zap.Strings("errors", fatal))
				}
				log.Warn("Config validation reported criticals — non-production profile, continuing",
					zap.String("profile", cfg.EffectiveProfile()), zap.Strings("criticals", fatal))
			}
			if cfg.Debug {
				log.Warn("Production config validation failed (debug mode, continuing anyway)", zap.Error(err))
			}
		} else {
			log.Fatal("Config validation failed", zap.Error(err))
		}
	}
	log.Info("Configuration loaded",
		zap.String("mode", cfg.Mode),
		zap.String("service", cfg.ServiceName))

	// Postgres is the task authority. No in-memory fallback: a worker that
	// cannot see the task table must not run.
	dbConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.User, cfg.Database.Password,
		cfg.Database.Database, cfg.Database.SSLMode)
	sqlDB, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatal("Failed to open Postgres", zap.Error(err))
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := sqlDB.PingContext(pingCtx); err != nil {
		pingCancel()
		log.Fatal("Postgres unreachable — the isolated worker cannot fall back to in-memory state", zap.Error(err))
	}
	pingCancel()
	pg := storage.NewPostgresDBFromDB(sqlDB)
	pg.SetMaxOpenConns(cfg.Database.MaxConns)
	if cfg.Database.MaxIdleConns > 0 {
		pg.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime != "" {
		if dur, parseErr := time.ParseDuration(cfg.Database.ConnMaxLifetime); parseErr == nil {
			pg.SetConnMaxLifetime(dur)
		}
	}
	log.Info("Database connected", zap.String("host", cfg.Database.Host))

	// Object storage: read originals, write HLS segments. No fallback either —
	// without it the worker could neither fetch input nor publish output.
	objStorage, err := storage.NewMinIOStorage(storage.MinIOConfig{
		Endpoint:        cfg.Storage.Endpoint,
		AccessKeyID:     cfg.Storage.AccessKey,
		SecretAccessKey: cfg.Storage.SecretKey,
		UseSSL:          cfg.Storage.UseSSL,
	})
	if err != nil {
		log.Fatal("Object storage unreachable", zap.Error(err))
	}
	bucketCtx, bucketCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := objStorage.CreateBucket(bucketCtx, "streamgate"); err != nil {
		log.Warn("Failed to create streamgate bucket", zap.Error(err))
	}
	bucketCancel()
	log.Info("Object storage initialized", zap.String("endpoint", cfg.Storage.Endpoint))

	// JetStream is the only task transport. A memory fallback would accept
	// deliveries locally while the cluster's tasks stay queued forever, so a
	// connect failure is fatal, not a degradation.
	nq, err := storage.NewNATSTranscodingQueue(cfg.NATS.URL, log.Named("nats-queue"))
	if err != nil {
		log.Fatal("NATS JetStream unreachable — the isolated worker cannot consume cluster tasks", zap.Error(err))
	}

	ffmpegCfg := &transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     30 * time.Minute,
		// Media gate ceilings for untrusted originals, from config.
		MaxFileSize: cfg.Transcoding.MaxInputSize,
		MaxDuration: float64(cfg.Transcoding.MaxDurationSeconds),
		MaxWidth:    cfg.Transcoding.MaxWidth,
		MaxHeight:   cfg.Transcoding.MaxHeight,
		Threads:     cfg.Transcoding.FFmpegThreads,
	}
	ft := transcoder.NewFFmpegTranscoder(ffmpegCfg, log.Named("ffmpeg"))

	// SAME service options as the gateway's provideTranscodingService, so the
	// worker enforces identical bounds (input ceiling, worker/queue/owner caps)
	// on the consuming side.
	svc := service.NewTranscodingService(pg, nq,
		service.WithTranscoder(transcoder.NewVideoTranscoder(ft, log.Named("ffmpeg"))),
		service.WithStorage(objStorage),
		service.WithLogger(log),
		service.WithMaxInputBytes(cfg.Transcoding.MaxInputSize),
		service.WithMinWorkers(1),
		service.WithMaxWorkers(cfg.Transcoding.MaxWorkers),
		service.WithQueueLimit(cfg.Transcoding.MaxQueuePending),
		service.WithOwnerTaskLimit(cfg.Transcoding.MaxOwnerPending),
	)
	svc.StartWorker(log.Named("transcode-worker"))
	log.Info("Transcoding role: external worker — claims and executes tasks submitted by the app processes")

	// Minimal health endpoint for the compose healthcheck; no business routes.
	// ReadHeaderTimeout bounds header read time so a slow client cannot hold the
	// connection open indefinitely (Slowloris / CWE-400), gosec G112.
	healthServer := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Server.Port), ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","service":"transcoder"}`))
	})}
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Health server error", zap.Error(err))
		}
	}()
	log.Info("StreamGate Transcoder Service started", zap.Int("health_port", cfg.Server.Port))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.Info("Received shutdown signal, draining (max 30s)", zap.String("signal", sig.String()))

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Warn("Health server shutdown error", zap.Error(err))
	}
	shutdownCancel()

	// Close stops claiming, cancels in-flight work and waits for the drain
	// (bounded at 30s inside the service).
	svc.Close()
	if err := nq.Close(); err != nil {
		log.Warn("NATS queue close error", zap.Error(err))
	}
	if err := sqlDB.Close(); err != nil {
		log.Warn("Database close error", zap.Error(err))
	}
	log.Info("StreamGate Transcoder Service stopped gracefully")
}
