package gateway

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	contentv1 "github.com/rtcdance/streamgate/pkg/api/v1/content"
	nftv1 "github.com/rtcdance/streamgate/pkg/api/v1/nft"
	servicev1 "github.com/rtcdance/streamgate/pkg/api/v1/service"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	uploadv1 "github.com/rtcdance/streamgate/pkg/api/v1/upload"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"

	jwt "github.com/golang-jwt/jwt/v4"
)

type GRPCServices struct {
	AuthService    *service.AuthService
	Web3Service    *service.Web3Service
	NFTVerifier    middleware.NFTOwnershipChecker
	StreamingSvc   *service.StreamingService
	ContentService *service.ContentService
	SegmentStorage service.SegmentStorage
	UploadService  *service.UploadService
	TranscodingSvc *service.TranscodingService
	DB             Pinger
	Cache          Pinger
	Blacklist      middleware.TokenBlacklistChecker
}

type Pinger interface {
	Ping(ctx context.Context) error
}

func SetupGRPCServer(cfg *config.Config, log *zap.Logger, svcs *GRPCServices) *grpc.Server {
	jwtSecret := cfg.Auth.JWTSecret

	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(100 << 20),
		grpc.MaxSendMsgSize(100 << 20),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  30 * time.Second,
			Timeout:               10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			grpcRequestIDUnaryInterceptor(),
			grpcRecoveryInterceptor(log),
			grpcLoggingInterceptor(log),
			grpcAuthInterceptor(jwtSecret, svcs.Blacklist, log),
		),
		grpc.ChainStreamInterceptor(
			grpcRequestIDStreamInterceptor(),
			grpcStreamRecoveryInterceptor(log),
			grpcStreamLoggingInterceptor(log),
			grpcStreamAuthInterceptor(jwtSecret, svcs.Blacklist, log),
		),
	}

	// Add TLS credentials if configured
	if cfg.GRPC.TLSEnabled && cfg.GRPC.TLSCert != "" && cfg.GRPC.TLSKey != "" {
		creds, err := credentials.NewServerTLSFromFile(cfg.GRPC.TLSCert, cfg.GRPC.TLSKey)
		if err != nil {
			log.Fatal("Failed to load gRPC TLS credentials; TLS was explicitly enabled but credentials are invalid",
				zap.String("cert", cfg.GRPC.TLSCert),
				zap.Error(err))
		} else {
			opts = append(opts, grpc.Creds(creds))
			log.Info("gRPC TLS enabled")
		}
	}

	srv := grpc.NewServer(opts...)

	healthSvc := &healthGrpcServer{log: log, db: svcs.DB, cache: svcs.Cache}
	servicev1.RegisterHealthServiceServer(srv, healthSvc)

	// Register standard grpc.health.v1.Health for Kubernetes readiness probes.
	// The standard health server serves based on internal health checks.
	grpcHealthSvc := health.NewServer()
	grpcHealthSvc.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, grpcHealthSvc)

	if healthSvc != nil {
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				resp, err := healthSvc.Check(ctx, &servicev1.HealthCheckRequest{})
				cancel()
				if err != nil {
					grpcHealthSvc.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
					continue
				}
				if resp.Status == servicev1.HealthCheckResponse_SERVING {
					grpcHealthSvc.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
				} else {
					grpcHealthSvc.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
				}
			}
		}()
	}

	if svcs.AuthService != nil && svcs.Web3Service != nil {
		authSrv := &authGrpcServer{
			authSvc: svcs.AuthService,
			web3Svc: svcs.Web3Service,
			log:     log,
		}
		authv1.RegisterAuthServiceServer(srv, authSrv)
	}

	if svcs.NFTVerifier != nil {
		nftSrv := &nftGrpcServer{
			nftVerifier: svcs.NFTVerifier,
			web3Svc:     svcs.Web3Service,
			log:         log,
		}
		nftv1.RegisterNFTServiceServer(srv, nftSrv)
	}

	if svcs.ContentService != nil {
		contentSrv := &contentGrpcServer{
			contentSvc:   svcs.ContentService,
			transcodeSvc: svcs.TranscodingSvc,
			log:          log,
		}
		contentv1.RegisterContentServiceServer(srv, contentSrv)
	}

	if svcs.AuthService != nil && svcs.SegmentStorage != nil {
		streamingSrv := &streamingGrpcServer{
			authSvc:      svcs.AuthService,
			nftVerifier:  svcs.NFTVerifier,
			streamingSvc: svcs.StreamingSvc,
			segStore:     svcs.SegmentStorage,
			log:          log,
		}
		streamingv1.RegisterStreamingServiceServer(srv, streamingSrv)
	}

	if svcs.UploadService != nil {
		uploadSrv := &uploadGrpcServer{
			uploadSvc: svcs.UploadService,
			log:       log,
		}
		uploadv1.RegisterUploadServiceServer(srv, uploadSrv)
	}

	if cfg.Mode != "production" {
		reflection.Register(srv)
	}
	log.Info("gRPC server initialized with registered services")
	return srv
}

// ============================== Health Service ==============================

type healthGrpcServer struct {
	servicev1.UnimplementedHealthServiceServer
	log   *zap.Logger
	db    Pinger
	cache Pinger
}

func (s *healthGrpcServer) Check(ctx context.Context, req *servicev1.HealthCheckRequest) (*servicev1.HealthCheckResponse, error) {
	if s.db != nil {
		if err := s.db.Ping(ctx); err != nil {
			s.log.Warn("Health check: database unreachable", zap.Error(err))
			return &servicev1.HealthCheckResponse{
				Status: servicev1.HealthCheckResponse_NOT_SERVING,
			}, nil
		}
	}
	if s.cache != nil {
		if err := s.cache.Ping(ctx); err != nil {
			s.log.Warn("Health check: cache unreachable", zap.Error(err))
			return &servicev1.HealthCheckResponse{
				Status: servicev1.HealthCheckResponse_NOT_SERVING,
			}, nil
		}
	}
	return &servicev1.HealthCheckResponse{
		Status: servicev1.HealthCheckResponse_SERVING,
	}, nil
}

func (s *healthGrpcServer) Watch(req *servicev1.HealthCheckRequest, stream servicev1.HealthService_WatchServer) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status := s.servingStatus()
			if err := stream.Send(&servicev1.HealthCheckResponse{
				Status: status,
			}); err != nil {
				return err
			}
		}
	}
}

func (s *healthGrpcServer) servingStatus() servicev1.HealthCheckResponse_ServingStatus {
	checkCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if s.db != nil {
		if err := s.db.Ping(checkCtx); err != nil {
			return servicev1.HealthCheckResponse_NOT_SERVING
		}
	}
	if s.cache != nil {
		if err := s.cache.Ping(checkCtx); err != nil {
			return servicev1.HealthCheckResponse_NOT_SERVING
		}
	}
	return servicev1.HealthCheckResponse_SERVING
}

// ============================== Auth Service ==============================

type authGrpcServer struct {
	authv1.UnimplementedAuthServiceServer
	authSvc *service.AuthService
	web3Svc *service.Web3Service
	log     *zap.Logger
}

func (s *authGrpcServer) GetNonce(ctx context.Context, req *authv1.GetNonceRequest) (*authv1.GetNonceResponse, error) {
	chainID := int64(1)
	if req.ChainType == "solana" {
		chainID = -1
	}
	challenge, err := s.authSvc.GenerateWalletChallenge(ctx, req.WalletAddress, chainID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate challenge")
	}
	return &authv1.GetNonceResponse{
		Nonce:     challenge.ID,
		Message:   challenge.Message,
		ExpiresAt: challenge.ExpiresAt.Unix(),
	}, nil
}

func (s *authGrpcServer) VerifySignature(ctx context.Context, req *authv1.VerifySignatureRequest) (*authv1.VerifySignatureResponse, error) {
	token, err := s.authSvc.AuthenticateWithWallet(ctx, req.WalletAddress, req.Nonce, req.Signature, 0)
	if err != nil {
		return &authv1.VerifySignatureResponse{Valid: false}, nil
	}
	result, err := s.authSvc.VerifyToken(ctx, token)
	if err != nil || result == nil || !result.Valid {
		return &authv1.VerifySignatureResponse{Valid: false}, nil
	}
	return &authv1.VerifySignatureResponse{
		Valid:       true,
		AccessToken: token,
	}, nil
}

func (s *authGrpcServer) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*authv1.RefreshTokenResponse, error) {
	newToken, err := s.authSvc.RefreshToken(ctx, req.RefreshToken)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	return &authv1.RefreshTokenResponse{
		Success:     true,
		AccessToken: newToken,
	}, nil
}

func (s *authGrpcServer) RevokeToken(ctx context.Context, req *authv1.RevokeTokenRequest) (*authv1.RevokeTokenResponse, error) {
	if err := s.authSvc.RevokeToken(ctx, req.Token); err != nil {
		return nil, status.Error(codes.Internal, "failed to revoke token")
	}
	return &authv1.RevokeTokenResponse{Success: true}, nil
}

func (s *authGrpcServer) VerifyToken(ctx context.Context, req *authv1.VerifyTokenRequest) (*authv1.VerifyTokenResponse, error) {
	result, err := s.authSvc.VerifyToken(ctx, req.Token)
	if err != nil || result == nil || !result.Valid {
		return &authv1.VerifyTokenResponse{Valid: false}, nil
	}
	return &authv1.VerifyTokenResponse{
		Valid: true,
	}, nil
}

// ============================== NFT Service ==============================

type nftGrpcServer struct {
	nftv1.UnimplementedNFTServiceServer
	nftVerifier middleware.NFTOwnershipChecker
	web3Svc     *service.Web3Service
	log         *zap.Logger
}

func (s *nftGrpcServer) VerifyOwnership(ctx context.Context, req *nftv1.VerifyOwnershipRequest) (*nftv1.VerifyOwnershipResponse, error) {
	owns, err := s.nftVerifier.VerifyNFTOwnership(ctx, req.ChainId, req.ContractAddress, req.TokenId, req.WalletAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, "nft verification failed")
	}
	var nftMeta *nftv1.NFTMetadata
	if owns && s.web3Svc != nil {
		nftInfo, err := s.web3Svc.GetNFT(ctx, req.ChainId, req.ContractAddress, req.TokenId)
		if err == nil && nftInfo != nil {
			nftMeta = &nftv1.NFTMetadata{
				Name:  nftInfo.Name,
				Image: nftInfo.URI,
			}
		}
	}
	return &nftv1.VerifyOwnershipResponse{
		OwnsNft:      owns,
		OwnerAddress: req.WalletAddress,
		Metadata:     nftMeta,
		VerifiedAt:   time.Now().Unix(),
	}, nil
}

func (s *nftGrpcServer) GetNFTBalance(ctx context.Context, req *nftv1.GetNFTBalanceRequest) (*nftv1.GetNFTBalanceResponse, error) {
	balance, err := s.nftVerifier.GetNFTBalance(ctx, req.ChainId, req.ContractAddress, req.WalletAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get nft balance")
	}
	return &nftv1.GetNFTBalanceResponse{
		Balance: balance.Int64(),
	}, nil
}

func (s *nftGrpcServer) GetNFTMetadata(ctx context.Context, req *nftv1.GetNFTMetadataRequest) (*nftv1.GetNFTMetadataResponse, error) {
	if s.web3Svc == nil {
		return nil, status.Error(codes.Unavailable, "web3 service not available")
	}
	nftInfo, err := s.web3Svc.GetNFT(ctx, req.ChainId, req.ContractAddress, req.TokenId)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get nft metadata")
	}
	if nftInfo == nil {
		return &nftv1.GetNFTMetadataResponse{Found: false}, nil
	}
	return &nftv1.GetNFTMetadataResponse{
		Metadata: &nftv1.NFTMetadata{
			Name:  nftInfo.Name,
			Image: nftInfo.URI,
		},
		Found: true,
	}, nil
}

func validatePagination(page, pageSize int) (p, ps int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	p = page
	ps = pageSize
	return
}

func validateID(id string) error {
	if strings.ContainsAny(id, "./\\") {
		return status.Error(codes.InvalidArgument, "id contains invalid characters")
	}
	return nil
}

func (s *nftGrpcServer) ListUserNFTs(ctx context.Context, req *nftv1.ListUserNFTsRequest) (*nftv1.ListUserNFTsResponse, error) {
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		wallet = req.WalletAddress
	}
	if wallet == "" {
		return nil, status.Error(codes.InvalidArgument, "wallet_address is required")
	}

	page, pageSize := validatePagination(int(req.Page), int(req.PageSize))

	var items []*nftv1.NFTItem
	if s.web3Svc != nil {
		indexer := s.web3Svc.GetEventIndexer()
		if indexer != nil {
			events := indexer.GetEventsByType("Transfer")
			seen := make(map[string]bool)
			type nftCandidate struct {
				contract string
				tokenID  string
			}
			var candidates []nftCandidate
			for _, evt := range events {
				if evt.Decoded == nil {
					continue
				}
				toAddr, _ := evt.Decoded["to"].(string)
				if !strings.EqualFold(toAddr, wallet) {
					continue
				}
				contract := evt.ContractAddress
				tokenID := ""
				if tid, ok := evt.Decoded["tokenId"].(string); ok {
					tokenID = tid
				} else if tid, ok := evt.Decoded["token_id"].(string); ok {
					tokenID = tid
				}
				if tokenID == "" {
					continue
				}
				key := contract + ":" + tokenID
				if seen[key] {
					continue
				}
				seen[key] = true
				candidates = append(candidates, nftCandidate{contract: contract, tokenID: tokenID})
			}

			type nftResult struct {
				item *nftv1.NFTItem
				ok   bool
			}
			resultCh := make(chan nftResult, len(candidates))
			sem := make(chan struct{}, 5)
			var wg sync.WaitGroup
			for _, c := range candidates {
				wg.Add(1)
				sem <- struct{}{}
				go func(contract, tokenID string) {
					defer wg.Done()
					defer func() { <-sem }()
					owns, err := s.nftVerifier.VerifyNFTOwnership(ctx, req.ChainId, contract, tokenID, wallet)
					if err != nil || !owns {
						resultCh <- nftResult{ok: false}
						return
					}
					item := &nftv1.NFTItem{
						ContractAddress: contract,
						TokenId:         tokenID,
					}
					nftInfo, err := s.web3Svc.GetNFT(ctx, req.ChainId, contract, tokenID)
					if err == nil && nftInfo != nil {
						item.Name = nftInfo.Name
						item.Image = nftInfo.URI
					}
					resultCh <- nftResult{item: item, ok: true}
				}(c.contract, c.tokenID)
			}
			go func() {
				wg.Wait()
				close(resultCh)
			}()
			for r := range resultCh {
				if r.ok && r.item != nil {
					items = append(items, r.item)
				}
			}
		}
	}

	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return &nftv1.ListUserNFTsResponse{
		Nfts:     items[start:end],
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}, nil
}

func (s *nftGrpcServer) GetContractInfo(ctx context.Context, req *nftv1.GetContractInfoRequest) (*nftv1.GetContractInfoResponse, error) {
	if req.ContractAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "contract_address is required")
	}
	if req.ChainId == 0 {
		return nil, status.Error(codes.InvalidArgument, "chain_id is required")
	}

	if s.web3Svc == nil {
		return nil, status.Error(codes.Unavailable, "web3 service not available")
	}

	nftInfo, err := s.web3Svc.GetNFT(ctx, req.ChainId, req.ContractAddress, "1")
	if err != nil {
		return &nftv1.GetContractInfoResponse{Found: false}, nil
	}

	contractType := "unknown"
	if s.nftVerifier != nil {
		contractType = s.web3Svc.DetectContractType(ctx, req.ChainId, req.ContractAddress)
	}

	chainName := ""
	chains := s.web3Svc.GetSupportedChains()
	for _, c := range chains {
		if c.ID == req.ChainId {
			chainName = c.Name
			break
		}
	}

	info := &nftv1.ContractInfo{
		Address:      req.ContractAddress,
		Name:         nftInfo.Name,
		Symbol:       nftInfo.Symbol,
		ContractType: contractType,
		ChainId:      req.ChainId,
		ChainName:    chainName,
	}

	return &nftv1.GetContractInfoResponse{
		Info:  info,
		Found: true,
	}, nil
}

// ============================== Content Service ==============================

type contentGrpcServer struct {
	contentv1.UnimplementedContentServiceServer
	contentSvc   *service.ContentService
	transcodeSvc *service.TranscodingService
	log          *zap.Logger
}

func (s *contentGrpcServer) GetContent(ctx context.Context, req *contentv1.GetContentRequest) (*contentv1.GetContentResponse, error) {
	if req.ContentId == "" {
		return nil, status.Error(codes.InvalidArgument, "content_id is required")
	}
	if err := validateID(req.ContentId); err != nil {
		return nil, err
	}
	svcContent, err := s.contentSvc.GetContent(ctx, req.ContentId)
	if err != nil || svcContent == nil {
		return nil, status.Error(codes.NotFound, "content not found")
	}
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" || !strings.EqualFold(svcContent.OwnerID, wallet) {
		return nil, status.Error(codes.PermissionDenied, "not authorized to access this content")
	}
	pbContent := &contentv1.Content{
		Id:    svcContent.ID,
		Title: svcContent.Title,
	}
	return &contentv1.GetContentResponse{
		Content: pbContent,
	}, nil
}

func (s *contentGrpcServer) VerifyAccess(ctx context.Context, req *contentv1.VerifyAccessRequest) (*contentv1.VerifyAccessResponse, error) {
	return nil, status.Error(codes.Unimplemented, "VerifyAccess not yet implemented")
}

func (s *contentGrpcServer) GetTranscodeStatus(ctx context.Context, req *contentv1.GetTranscodeStatusRequest) (*contentv1.GetTranscodeStatusResponse, error) {
	if s.transcodeSvc == nil {
		return nil, status.Error(codes.Unavailable, "transcoding service not available")
	}
	if req.ContentId == "" {
		return nil, status.Error(codes.InvalidArgument, "content_id is required")
	}
	if err := validateID(req.ContentId); err != nil {
		return nil, err
	}
	task, err := s.transcodeSvc.GetTranscodingStatus(ctx, req.ContentId)
	if err != nil || task == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" || !strings.EqualFold(task.OwnerWallet, wallet) {
		return nil, status.Error(codes.PermissionDenied, "not authorized to access this task")
	}
	return &contentv1.GetTranscodeStatusResponse{
		Status:    task.Status,
		Progress:  int32(task.Progress),
		StartedAt: task.CreatedAt.Unix(),
	}, nil
}

func (s *contentGrpcServer) ListContent(ctx context.Context, req *contentv1.ListContentRequest) (*contentv1.ListContentResponse, error) {
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required")
	}
	limit := int(req.PageSize)
	offset := (int(req.Page) - 1) * limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	contents, err := s.contentSvc.ListContents(ctx, wallet, limit, offset)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list content")
	}
	total, _ := s.contentSvc.CountContents(ctx, wallet)
	totalInt32 := int32(total)
	if total > math.MaxInt32 {
		totalInt32 = math.MaxInt32
	}
	var pbContents []*contentv1.Content
	for _, c := range contents {
		pbContents = append(pbContents, &contentv1.Content{
			Id:    c.ID,
			Title: c.Title,
		})
	}
	return &contentv1.ListContentResponse{
		Contents: pbContents,
		Total:    totalInt32,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}

func (s *contentGrpcServer) DeleteContent(ctx context.Context, req *contentv1.DeleteContentRequest) (*contentv1.DeleteContentResponse, error) {
	if req.ContentId == "" {
		return nil, status.Error(codes.InvalidArgument, "content_id is required")
	}
	if err := validateID(req.ContentId); err != nil {
		return nil, err
	}
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required")
	}

	content, err := s.contentSvc.GetContent(ctx, req.ContentId)
	if err != nil || content == nil {
		return nil, status.Error(codes.NotFound, "content not found")
	}
	if !strings.EqualFold(content.OwnerID, wallet) {
		return nil, status.Error(codes.PermissionDenied, "not authorized to delete this content")
	}

	if err := s.contentSvc.DeleteContent(ctx, req.ContentId, wallet); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete content")
	}
	return &contentv1.DeleteContentResponse{Success: true}, nil
}

// ============================== Streaming Service ==============================

type streamingGrpcServer struct {
	streamingv1.UnimplementedStreamingServiceServer
	authSvc      *service.AuthService
	nftVerifier  middleware.NFTOwnershipChecker
	streamingSvc *service.StreamingService
	segStore     service.SegmentStorage
	log          *zap.Logger
}

func (s *streamingGrpcServer) GetStreamURL(ctx context.Context, req *streamingv1.GetStreamURLRequest) (*streamingv1.GetStreamURLResponse, error) {
	_ = req
	return nil, status.Error(codes.Unimplemented, "GetStreamURL not yet implemented")
}

func (s *streamingGrpcServer) GetManifest(ctx context.Context, req *streamingv1.GetManifestRequest) (*streamingv1.GetManifestResponse, error) {
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required")
	}
	if req.ContentId == "" {
		return nil, status.Error(codes.InvalidArgument, "content_id is required")
	}
	if err := validateID(req.ContentId); err != nil {
		return nil, err
	}

	var contract, tokenID string
	var chainID int64 = 1
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("x-nft-contract"); len(v) > 0 {
			contract = v[0]
		}
		if v := md.Get("x-nft-token-id"); len(v) > 0 {
			tokenID = v[0]
		}
		if v := md.Get("x-nft-chain-id"); len(v) > 0 {
			if parsed, err := fmt.Sscanf(v[0], "%d", &chainID); parsed != 1 || err != nil {
				chainID = 1
			}
		}
	}

	if contract == "" {
		return nil, status.Error(codes.PermissionDenied, "NFT contract address required; provide x-nft-contract metadata header")
	}
	if s.nftVerifier == nil {
		return nil, status.Error(codes.Internal, "NFT verification service unavailable; access denied for safety")
	}
	owned, err := s.nftVerifier.VerifyNFTOwnership(ctx, chainID, contract, tokenID, wallet)
	if err != nil {
		s.log.Warn("NFT ownership verification failed",
			zap.String("wallet", wallet),
			zap.String("contract", contract),
			zap.Error(err))
		return nil, status.Error(codes.PermissionDenied, "NFT ownership verification failed")
	}
	if !owned {
		return nil, status.Error(codes.PermissionDenied, "NFT ownership required")
	}

	playbackToken, err := s.authSvc.GeneratePlaybackToken(ctx, wallet, req.ContentId, contract, tokenID, chainID, 2*time.Minute, "")
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate playback token")
	}

	segmentPrefix := fmt.Sprintf("streams/%s/", req.ContentId)
	qualitySegments := make(map[string][]string)
	if s.segStore != nil {
		objs, err := s.segStore.ListObjects(ctx, "streamgate", segmentPrefix)
		if err != nil {
			s.log.Warn("Failed to list segments",
				zap.String("content_id", req.ContentId),
				zap.Error(err))
		}
		for _, key := range objs {
			if !strings.HasSuffix(key, ".ts") {
				continue
			}
			rel := strings.TrimPrefix(key, segmentPrefix)
			parts := strings.SplitN(rel, "/", 2)
			quality := "default"
			segName := rel
			if len(parts) == 2 {
				quality = parts[0]
				segName = parts[1]
			}
			qualitySegments[quality] = append(qualitySegments[quality], segName)
		}
	}
	if len(qualitySegments) == 0 {
		return nil, status.Error(codes.NotFound, "content not ready; transcode may still be processing")
	}

	var manifest string
	segments := make([]*streamingv1.SegmentInfo, 0)
	manifest, err = s.streamingSvc.GenerateHLSPlaylist(req.ContentId, qualitySegments, playbackToken)
	if err != nil {
		s.log.Error("failed to generate HLS playlist",
			zap.String("content_id", req.ContentId),
			zap.String("wallet", wallet),
			zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to generate stream manifest")
	}
	for q, segs := range qualitySegments {
		for _, seg := range segs {
			name := seg
			if idx := strings.LastIndex(seg, "/"); idx >= 0 {
				name = seg[idx+1:]
			}
			segments = append(segments, &streamingv1.SegmentInfo{
				SegmentId: name,
				Quality:   q,
				Duration:  4,
				Url:       fmt.Sprintf("/api/v1/streaming/%s/segment/%s?playback_token=%s", req.ContentId, name, playbackToken),
			})
		}
	}

	return &streamingv1.GetManifestResponse{
		Manifest:    manifest,
		ContentType: "application/vnd.apple.mpegurl",
		Segments:    segments,
	}, nil
}

func (s *streamingGrpcServer) GetSegment(ctx context.Context, req *streamingv1.GetSegmentRequest) (*streamingv1.GetSegmentResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		tokens := md.Get("playback-token")
		if len(tokens) == 0 || tokens[0] == "" {
			return nil, status.Error(codes.Unauthenticated, "missing playback token")
		}
		if _, err := s.authSvc.ValidatePlaybackToken(ctx, tokens[0], req.ContentId, ""); err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid playback token")
		}
	} else {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	if strings.Contains(req.SegmentId, "..") || strings.Contains(req.SegmentId, "/") || strings.Contains(req.SegmentId, "\\") {
		return nil, status.Error(codes.InvalidArgument, "invalid segment id")
	}
	if err := validateID(req.ContentId); err != nil {
		return nil, err
	}

	objKey := fmt.Sprintf("streams/%s/%s", req.ContentId, req.SegmentId)
	if s.segStore == nil {
		return nil, status.Error(codes.Unavailable, "segment storage not available")
	}
	data, err := s.segStore.Download(ctx, "streamgate", objKey)
	if err != nil {
		return nil, status.Error(codes.NotFound, "segment not found")
	}
	return &streamingv1.GetSegmentResponse{
		Data:        data,
		ContentType: "video/mp2t",
		Size:        int64(len(data)),
	}, nil
}

func (s *streamingGrpcServer) StartStream(ctx context.Context, req *streamingv1.StartStreamRequest) (*streamingv1.StartStreamResponse, error) {
	return nil, status.Error(codes.Unimplemented, "StartStream not yet implemented")
}

func (s *streamingGrpcServer) StopStream(ctx context.Context, req *streamingv1.StopStreamRequest) (*streamingv1.StopStreamResponse, error) {
	return nil, status.Error(codes.Unimplemented, "StopStream not yet implemented")
}

func (s *streamingGrpcServer) GetStreamStats(ctx context.Context, req *streamingv1.GetStreamStatsRequest) (*streamingv1.GetStreamStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetStreamStats not yet implemented")
}

// ============================== Upload Service ==============================

type uploadGrpcServer struct {
	uploadv1.UnimplementedUploadServiceServer
	uploadSvc *service.UploadService
	log       *zap.Logger
}

func (s *uploadGrpcServer) InitUpload(ctx context.Context, req *uploadv1.InitUploadRequest) (*uploadv1.InitUploadResponse, error) {
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required")
	}
	if req.Filename == "" {
		return nil, status.Error(codes.InvalidArgument, "filename is required")
	}
	if req.FileSize <= 0 {
		return nil, status.Error(codes.InvalidArgument, "file_size must be positive")
	}
	const maxFileSize int64 = 500 * 1024 * 1024
	if req.FileSize > maxFileSize {
		return nil, status.Error(codes.InvalidArgument, "file size exceeds 500MB limit")
	}
	if req.ChunkSize <= 0 {
		return nil, status.Error(codes.InvalidArgument, "chunk_size must be positive")
	}

	totalChunks := int((req.FileSize + int64(req.ChunkSize) - 1) / int64(req.ChunkSize))
	if totalChunks <= 0 {
		totalChunks = 1
	}
	if totalChunks > 10000 {
		return nil, status.Error(codes.InvalidArgument, "too many chunks")
	}
	if totalChunks > math.MaxInt32 {
		return nil, status.Error(codes.InvalidArgument, "file too large for chunked upload")
	}
	uploadID, err := s.uploadSvc.InitiateChunkedUpload(ctx, req.Filename, req.FileSize, totalChunks, wallet)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to init upload")
	}
	return &uploadv1.InitUploadResponse{
		UploadId:   uploadID,
		ChunkCount: int32(totalChunks),
		ExpiresAt:  time.Now().Add(24 * time.Hour).Unix(),
	}, nil
}

func (s *uploadGrpcServer) UploadPart(stream uploadv1.UploadService_UploadPartServer) error {
	wallet := grpcWalletFromContext(stream.Context())
	if wallet == "" {
		return status.Error(codes.Unauthenticated, "wallet address required")
	}

	var uploadID string
	ownershipVerified := false
	msgCount := 0
	const maxMessages = 10001
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Error(codes.Internal, "stream read failed")
		}
		msgCount++
		if msgCount > maxMessages {
			return status.Error(codes.InvalidArgument, "too many chunks in stream")
		}
		if uploadID == "" {
			uploadID = req.UploadId
		}
		if strings.Contains(req.UploadId, "..") || strings.Contains(req.UploadId, "/") || strings.Contains(req.UploadId, "\\") {
			return status.Error(codes.InvalidArgument, "invalid upload_id")
		}
		if req.PartNumber < 1 {
			return status.Error(codes.InvalidArgument, "part_number must be >= 1")
		}
		if !ownershipVerified && uploadID != "" {
			info, infoErr := s.uploadSvc.GetUploadStatus(stream.Context(), uploadID)
			if infoErr != nil {
				return status.Error(codes.NotFound, "upload not found")
			}
			if !strings.EqualFold(info.OwnerID, wallet) {
				return status.Error(codes.PermissionDenied, "not authorized to upload to this resource")
			}
			ownershipVerified = true
		}
		if err := s.uploadSvc.UploadChunk(stream.Context(), req.UploadId, int(req.PartNumber)-1, req.Data, ""); err != nil {
			return status.Error(codes.Internal, "upload chunk failed")
		}
	}
	return stream.SendAndClose(&uploadv1.UploadPartResponse{
		Success: true,
	})
}

func (s *uploadGrpcServer) CompleteUpload(ctx context.Context, req *uploadv1.CompleteUploadRequest) (*uploadv1.CompleteUploadResponse, error) {
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required")
	}

	info, err := s.uploadSvc.GetUploadStatus(ctx, req.UploadId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "upload not found")
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		return nil, status.Error(codes.PermissionDenied, "not authorized to complete this upload")
	}

	totalParts := len(req.Parts)
	if totalParts <= 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one part is required")
	}
	if err := s.uploadSvc.CompleteChunkedUpload(ctx, req.UploadId, totalParts); err != nil {
		return nil, status.Error(codes.Internal, "failed to complete upload")
	}
	return &uploadv1.CompleteUploadResponse{
		Success: true,
	}, nil
}

func (s *uploadGrpcServer) AbortUpload(ctx context.Context, req *uploadv1.AbortUploadRequest) (*uploadv1.AbortUploadResponse, error) {
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required")
	}

	info, err := s.uploadSvc.GetUploadStatus(ctx, req.UploadId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "upload not found")
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		return nil, status.Error(codes.PermissionDenied, "not authorized to abort this upload")
	}

	if err := s.uploadSvc.DeleteUpload(ctx, req.UploadId); err != nil {
		return nil, status.Error(codes.Internal, "failed to abort upload")
	}
	return &uploadv1.AbortUploadResponse{Success: true}, nil
}

func (s *uploadGrpcServer) GetUploadStatus(ctx context.Context, req *uploadv1.GetUploadStatusRequest) (*uploadv1.GetUploadStatusResponse, error) {
	wallet := grpcWalletFromContext(ctx)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required")
	}

	info, err := s.uploadSvc.GetUploadStatus(ctx, req.UploadId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "upload not found")
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		return nil, status.Error(codes.PermissionDenied, "not authorized to view this upload")
	}

	resp := &uploadv1.GetUploadStatusResponse{
		UploadId:   info.ID,
		Status:     info.Status,
		TotalBytes: info.Size,
		StartedAt:  info.CreatedAt.Unix(),
		UpdatedAt:  info.UpdatedAt.Unix(),
	}

	if info.Status == "uploading" {
		chunks, err := s.uploadSvc.GetChunkStatuses(ctx, req.UploadId)
		if err == nil && len(chunks) > 0 {
			uploaded := int32(0)
			for _, c := range chunks {
				if c.Uploaded {
					uploaded++
				}
			}
			resp.PartsUploaded = uploaded
			resp.TotalParts = int32(len(chunks))
		}
	}

	return resp, nil
}

var grpcNoAuthMethods = map[string]bool{
	"/auth.AuthService/GetNonce":        true,
	"/auth.AuthService/VerifySignature": true,
	"/health.HealthService/Check":       true,
	"/health.HealthService/Watch":       true,
}

type grpcContextKey string

const grpcWalletKey grpcContextKey = "grpc_wallet_address"

func grpcWalletFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(grpcWalletKey).(string); ok {
		return v
	}
	return ""
}

// grpcRequestIDKey is the context key for the gRPC request ID.
type grpcRequestIDKey struct{}

const grpcRequestIDHeader = "x-request-id"

// grpcRequestIDUnaryInterceptor extracts x-request-id from gRPC metadata
// and injects it into the context for correlation with HTTP-side logging.
func grpcRequestIDUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get(grpcRequestIDHeader); len(vals) > 0 && vals[0] != "" {
				ctx = context.WithValue(ctx, grpcRequestIDKey{}, vals[0])
			}
		}
		return handler(ctx, req)
	}
}

// grpcRequestIDStreamInterceptor extracts x-request-id for streaming RPCs.
func grpcRequestIDStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if md, ok := metadata.FromIncomingContext(ss.Context()); ok {
			if vals := md.Get(grpcRequestIDHeader); len(vals) > 0 && vals[0] != "" {
				ctx := context.WithValue(ss.Context(), grpcRequestIDKey{}, vals[0])
				ws := &wrappedStream{ServerStream: ss, ctx: ctx}
				return handler(srv, ws)
			}
		}
		return handler(srv, ss)
	}
}

func grpcAuthInterceptor(jwtSecret string, blacklist middleware.TokenBlacklistChecker, log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		newCtx, err := verifyGRPCJWT(ctx, info.FullMethod, jwtSecret, blacklist)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
}

func grpcStreamAuthInterceptor(jwtSecret string, blacklist middleware.TokenBlacklistChecker, log *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		newCtx, err := verifyGRPCJWT(ss.Context(), info.FullMethod, jwtSecret, blacklist)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: newCtx})
	}
}

func verifyGRPCJWT(ctx context.Context, fullMethod string, jwtSecret string, blacklist middleware.TokenBlacklistChecker) (context.Context, error) {
	if grpcNoAuthMethods[fullMethod] {
		return ctx, nil
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization token")
	}

	tokenStr := strings.TrimPrefix(values[0], "Bearer ")
	tokenStr = strings.TrimSpace(tokenStr)
	if tokenStr == "" {
		return nil, status.Error(codes.Unauthenticated, "empty authorization token")
	}

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	if blacklist != nil {
		if jti, ok := claims["jti"].(string); ok && jti != "" {
			if blacklist.IsTokenRevoked(ctx, jti) {
				return nil, status.Error(codes.Unauthenticated, "token has been revoked")
			}
		}
	}

	wallet, _ := claims["wallet_address"].(string)
	if wallet == "" {
		return nil, status.Error(codes.Unauthenticated, "wallet address required in token")
	}

	ctx = context.WithValue(ctx, grpcWalletKey, wallet)
	return ctx, nil
}

func grpcRecoveryInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("gRPC panic recovered",
					zap.String("method", info.FullMethod),
					zap.Any("panic", r),
				)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

func grpcLoggingInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		latency := time.Since(start)
		code := status.Code(err)
		fields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.String("code", code.String()),
			zap.Duration("latency", latency),
		}
		if reqID, ok := ctx.Value(grpcRequestIDKey{}).(string); ok && reqID != "" {
			fields = append(fields, zap.String("request_id", reqID))
		}
		if err != nil {
			fields = append(fields, zap.Error(err))
		}
		if code != codes.OK {
			log.Warn("gRPC unary call", fields...)
		}
		return resp, err
	}
}

func grpcStreamRecoveryInterceptor(log *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("gRPC stream panic recovered",
					zap.String("method", info.FullMethod),
					zap.Any("panic", r),
				)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, ss)
	}
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

func grpcStreamLoggingInterceptor(log *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		latency := time.Since(start)
		code := status.Code(err)
		if code != codes.OK {
			log.Warn("gRPC stream call",
				zap.String("method", info.FullMethod),
				zap.String("code", code.String()),
				zap.Duration("latency", latency),
				zap.Error(err),
			)
		}
		return err
	}
}

var _ *service.UploadService
