package gateway

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	authv1 "streamgate/pkg/api/v1/auth"
	contentv1 "streamgate/pkg/api/v1/content"
	nftv1 "streamgate/pkg/api/v1/nft"
	servicev1 "streamgate/pkg/api/v1/service"
	streamingv1 "streamgate/pkg/api/v1/streaming"
	uploadv1 "streamgate/pkg/api/v1/upload"
	"streamgate/pkg/core/config"
	"streamgate/pkg/middleware"
	"streamgate/pkg/service"
	"streamgate/pkg/web3"

	jwt "github.com/golang-jwt/jwt/v4"
)

type GRPCServices struct {
	AuthService    *service.AuthService
	Web3Service    *service.Web3Service
	NFTVerifier    middleware.NFTOwnershipChecker
	ContentService *service.ContentService
	SegmentStorage service.SegmentStorage
	UploadService  *service.UploadService
	TranscodingSvc *service.TranscodingService
	Blacklist      middleware.TokenBlacklistChecker
}

func SetupGRPCServer(cfg *config.Config, log *zap.Logger, svcs *GRPCServices) *grpc.Server {
	jwtSecret := cfg.Auth.JWTSecret

	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(100<<20),
		grpc.ChainUnaryInterceptor(
			grpcRecoveryInterceptor(log),
			grpcLoggingInterceptor(log),
			grpcAuthInterceptor(jwtSecret, svcs.Blacklist, log),
		),
		grpc.ChainStreamInterceptor(
			grpcStreamRecoveryInterceptor(log),
			grpcStreamLoggingInterceptor(log),
			grpcStreamAuthInterceptor(jwtSecret, svcs.Blacklist, log),
		),
	)

	healthSvc := &healthGrpcServer{log: log}
	servicev1.RegisterHealthServiceServer(srv, healthSvc)

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
			authSvc:    svcs.AuthService,
			nftVerifier: svcs.NFTVerifier,
			segStore:   svcs.SegmentStorage,
			log:        log,
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

	reflection.Register(srv)
	log.Info("gRPC server initialized with registered services")
	return srv
}

// ============================== Health Service ==============================

type healthGrpcServer struct {
	servicev1.UnimplementedHealthServiceServer
	log *zap.Logger
}

func (s *healthGrpcServer) Check(ctx context.Context, req *servicev1.HealthCheckRequest) (*servicev1.HealthCheckResponse, error) {
	return &servicev1.HealthCheckResponse{
		Status: servicev1.HealthCheckResponse_SERVING,
	}, nil
}

func (s *healthGrpcServer) Watch(req *servicev1.HealthCheckRequest, stream servicev1.HealthService_WatchServer) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			if err := stream.Send(&servicev1.HealthCheckResponse{
				Status: servicev1.HealthCheckResponse_SERVING,
			}); err != nil {
				return err
			}
		}
	}
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
	token, err := s.authSvc.AuthenticateWithWallet(ctx, req.WalletAddress, req.Nonce, req.Signature)
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
	balInt64 := balance.Int64()
	if balInt64 > math.MaxInt32 {
		balInt64 = math.MaxInt32
	}
	return &nftv1.GetNFTBalanceResponse{
		Balance: int32(balInt64),
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

				owns, err := s.nftVerifier.VerifyNFTOwnership(ctx, req.ChainId, contract, tokenID, wallet)
				if err != nil || !owns {
					continue
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
				items = append(items, item)
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
		client, err := s.web3Svc.GetMultiChainManager().GetClient(req.ChainId)
		if err == nil {
			ts := web3.DetectTokenStandard(ctx, client.GetEthClient(), req.ContractAddress, s.log)
			switch ts {
			case web3.TokenStandardERC721:
				contractType = "ERC-721"
			case web3.TokenStandardERC1155:
				contractType = "ERC-1155"
			}
		}
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

	if err := s.contentSvc.DeleteContent(ctx, req.ContentId); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete content")
	}
	return &contentv1.DeleteContentResponse{Success: true}, nil
}

// ============================== Streaming Service ==============================

type streamingGrpcServer struct {
	streamingv1.UnimplementedStreamingServiceServer
	authSvc    *service.AuthService
	nftVerifier middleware.NFTOwnershipChecker
	segStore   service.SegmentStorage
	log        *zap.Logger
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

	if s.nftVerifier != nil && contract != "" {
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
	}

	playbackToken, err := s.authSvc.GeneratePlaybackToken(wallet, req.ContentId, contract, tokenID, chainID, 2*time.Minute)
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
	if len(qualitySegments) == 1 {
		for q, segs := range qualitySegments {
			manifest = buildSimplePlaylist(req.ContentId, segs, playbackToken)
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
	} else {
		manifest = buildMasterPlaylist(req.ContentId, qualitySegments, playbackToken)
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
		if _, err := s.authSvc.ValidatePlaybackToken(tokens[0], req.ContentId); err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid playback token")
		}
	} else {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	if strings.Contains(req.SegmentId, "..") || strings.Contains(req.SegmentId, "/") || strings.Contains(req.SegmentId, "\\") {
		return nil, status.Error(codes.InvalidArgument, "invalid segment id")
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
	var uploadID string
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

func grpcAuthInterceptor(jwtSecret string, blacklist middleware.TokenBlacklistChecker, log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if grpcNoAuthMethods[info.FullMethod] {
			return handler(ctx, req)
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

		return handler(ctx, req)
	}
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
		if code != codes.OK {
			log.Warn("gRPC unary call",
				zap.String("method", info.FullMethod),
				zap.String("code", code.String()),
				zap.Duration("latency", latency),
				zap.Error(err),
			)
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

func grpcStreamAuthInterceptor(jwtSecret string, blacklist middleware.TokenBlacklistChecker, log *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if grpcNoAuthMethods[info.FullMethod] {
			return handler(srv, ss)
		}

		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		values := md.Get("authorization")
		if len(values) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization token")
		}

		tokenStr := strings.TrimPrefix(values[0], "Bearer ")
		tokenStr = strings.TrimSpace(tokenStr)
		if tokenStr == "" {
			return status.Error(codes.Unauthenticated, "empty authorization token")
		}

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			return status.Error(codes.Unauthenticated, "invalid token")
		}

		if blacklist != nil {
			if jti, ok := claims["jti"].(string); ok && jti != "" {
				if blacklist.IsTokenRevoked(ss.Context(), jti) {
					return status.Error(codes.Unauthenticated, "token has been revoked")
				}
			}
		}

		wallet, _ := claims["wallet_address"].(string)
		if wallet == "" {
			return status.Error(codes.Unauthenticated, "wallet address required in token")
		}

		ctx := context.WithValue(ss.Context(), grpcWalletKey, wallet)
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
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
