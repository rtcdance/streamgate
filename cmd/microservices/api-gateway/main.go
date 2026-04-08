package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	apiV1 "streamgate/pkg/api/v1"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/logger"
	"streamgate/pkg/middleware"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/service"
	"streamgate/pkg/web3"
)

type nftAccessVerifier interface {
	VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress string, tokenID string, ownerAddress string) (bool, error)
	GetNFTBalance(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (int64, error)
}

type web3StatusProvider interface {
	GetRPCStatuses() map[int64][]web3.RPCStatus
	GetSupportedChains() []*web3.ChainConfig
}

type cachedNFTAccess struct {
	HasNFT    bool
	Balance   int64
	ExpiresAt time.Time
}

type nftAccessCache struct {
	mu      sync.RWMutex
	entries map[string]cachedNFTAccess
}

func newNFTAccessCache() *nftAccessCache {
	return &nftAccessCache{
		entries: make(map[string]cachedNFTAccess),
	}
}

func (c *nftAccessCache) get(key string) (cachedNFTAccess, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return cachedNFTAccess{}, false
	}
	if time.Now().After(entry.ExpiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return cachedNFTAccess{}, false
	}
	return entry, true
}

func (c *nftAccessCache) set(key string, entry cachedNFTAccess) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry
}

func nftCacheKey(chainID int64, wallet, contract, tokenID string) string {
	return strings.Join([]string{
		strconv.FormatInt(chainID, 10),
		strings.ToLower(strings.TrimSpace(wallet)),
		strings.ToLower(strings.TrimSpace(contract)),
		strings.TrimSpace(tokenID),
	}, ":")
}

func main() {
	// Initialize logger
	log := logger.NewDevelopmentLogger("streamgate-api-gateway")
	defer log.Sync()

	log.Info("Starting StreamGate API Gateway Service...")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Configure service
	cfg.Mode = "microservice"
	cfg.ServiceName = "api-gateway"
	cfg.Server.Port = 9090
	log.Info("Configuration loaded",
		zap.String("mode", cfg.Mode),
		zap.String("service", cfg.ServiceName),
		zap.Int("port", cfg.Server.Port))

	web3Service, err := service.NewWeb3Service(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize Web3 service", zap.Error(err))
	}
	challengeTTL := 5 * time.Minute
	if cfg.Auth.NonceExpiry != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.NonceExpiry); err == nil && parsed > 0 {
			challengeTTL = parsed
		}
	}
	var challengeStore service.ChallengeStore
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	if store, err := service.NewRedisChallengeStore(redisAddr, challengeTTL); err == nil {
		challengeStore = store
	} else {
		log.Warn("Falling back to in-memory challenge store", zap.Error(err))
	}
	authService := service.NewAuthServiceWithDeps(cfg.Auth.JWTSecret, nil, nil, challengeStore, challengeTTL)
	nftCache := newNFTAccessCache()
	transcodingService := service.NewTranscodingService(nil, service.NewMemoryTranscodingQueue())
	transcodingHandler := apiV1.NewTranscodingHandler(transcodingService)
	metricsCollector := monitoring.NewMetricsCollector(log)
	serviceMetrics := monitoring.NewServiceMetricsTracker(log)
	metricsExporter := monitoring.NewPrometheusExporter(metricsCollector, serviceMetrics, log)
	metricsHandler := monitoring.NewPrometheusMetricsHandler(metricsExporter, log)

	// Initialize HTTP router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Apply middleware
	middlewareSvc := middleware.NewService(log)
	router.Use(middlewareSvc.LoggingMiddleware())
	router.Use(middlewareSvc.RecoveryMiddleware())
	router.Use(middlewareSvc.CORSMiddleware())
	router.Use(middlewareSvc.RateLimitMiddleware())
	router.Use(func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		status := strconv.Itoa(c.Writer.Status())
		metricsCollector.IncrementCounter("http_requests_total", map[string]string{
			"method": c.Request.Method,
			"route":  route,
			"status": status,
		})
		serviceMetrics.RecordRequest("api-gateway", time.Since(startedAt).Milliseconds(), c.Writer.Status() < http.StatusInternalServerError)
	})

	// Register health check routes
	router.GET("/health", func(c *gin.Context) {
		metricsCollector.IncrementCounter("health_check_success_total", nil)
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "api-gateway",
			"timestamp": time.Now().Unix(),
		})
	})

	router.GET("/metrics", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; version=0.0.4", []byte(metricsHandler.ServeMetrics()))
	})

	router.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ready",
			"service": "api-gateway",
		})
	})

	// Register API routes
	registerAuthRoutes(router, log, authService)
	registerContentRoutes(router, log)
	registerNFTRoutes(router, log, web3Service, cfg.Web3.ChainID, nftCache)
	registerWeb3Routes(router, log, web3Service)
	registerStreamingRoutes(router, log, web3Service, authService, cfg.Web3.ChainID, nftCache)
	registerTranscodingRoutes(router, log, transcodingHandler)
	registerUploadRoutes(router, log)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Initialize gRPC server
	grpcListener, err := net.Listen("tcp", ":9091")
	if err != nil {
		log.Fatal("Failed to create gRPC listener", zap.Error(err))
	}

	grpcServer := grpc.NewServer()
	log.Info("gRPC server initialized", zap.Int("port", 9091))

	// Start HTTP server in goroutine
	go func() {
		log.Info("Starting HTTP server", zap.Int("port", cfg.Server.Port))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", zap.Error(err))
		}
	}()

	// Start gRPC server in goroutine
	go func() {
		log.Info("Starting gRPC server", zap.Int("port", 9091))
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Error("gRPC server error", zap.Error(err))
		}
	}()

	log.Info("StreamGate API Gateway Service started successfully",
		zap.Int("http_port", cfg.Server.Port),
		zap.Int("grpc_port", 9091))

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("Error shutting down HTTP server", zap.Error(err))
	}

	// Shutdown gRPC server
	grpcServer.GracefulStop()

	log.Info("StreamGate API Gateway Service stopped gracefully")
}

// registerAuthRoutes registers authentication routes
func registerAuthRoutes(router *gin.Engine, log *zap.Logger, authService *service.AuthService) {
	auth := router.Group("/api/v1/auth")
	{
		auth.POST("/challenge", func(c *gin.Context) {
			var req struct {
				Address string `json:"address"`
				Wallet  string `json:"wallet"`
				ChainID int64  `json:"chain_id"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
				return
			}
			wallet := req.Wallet
			if wallet == "" {
				wallet = req.Address
			}
			chainID := req.ChainID
			if chainID == 0 {
				chainID = 11155111
			}
			challenge, err := authService.GenerateWalletChallenge(wallet, chainID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"challenge_id": challenge.ID,
				"message":      challenge.Message,
				"expires_at":   challenge.ExpiresAt.Format(time.RFC3339),
				"wallet":       challenge.WalletAddress,
				"chain_id":     challenge.ChainID,
			})
		})
		auth.POST("/login", func(c *gin.Context) {
			var req struct {
				Address     string `json:"address"`
				Wallet      string `json:"wallet"`
				ChallengeID string `json:"challenge_id"`
				Signature   string `json:"signature"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
				return
			}
			wallet := req.Wallet
			if wallet == "" {
				wallet = req.Address
			}
			token, err := authService.AuthenticateWithWallet(wallet, req.ChallengeID, req.Signature)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"token": token, "wallet_address": wallet})
		})
		auth.POST("/logout", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "logout endpoint"})
		})
		auth.POST("/verify", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "verify endpoint"})
		})
		auth.GET("/profile", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "profile endpoint"})
		})
	}
	log.Info("Auth routes registered")
}

// registerContentRoutes registers content routes
func registerContentRoutes(router *gin.Engine, log *zap.Logger) {
	content := router.Group("/api/v1/content")
	{
		content.GET("", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "list content endpoint"})
		})
		content.GET("/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "get content endpoint"})
		})
		content.POST("", func(c *gin.Context) {
			c.JSON(http.StatusCreated, gin.H{"message": "create content endpoint"})
		})
		content.PUT("/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "update content endpoint"})
		})
		content.DELETE("/:id", func(c *gin.Context) {
			c.JSON(http.StatusNoContent, gin.H{"message": "delete content endpoint"})
		})
	}
	log.Info("Content routes registered")
}

// registerNFTRoutes registers NFT routes
func registerNFTRoutes(router *gin.Engine, log *zap.Logger, web3Service nftAccessVerifier, defaultChainID int64, cache *nftAccessCache) {
	nft := router.Group("/api/v1/nft")
	{
		nft.GET("", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "list NFT endpoint"})
		})
		nft.GET("/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "get NFT endpoint"})
		})
		nft.POST("/verify", func(c *gin.Context) {
			var req struct {
				ChainID         int64  `json:"chain_id"`
				Address         string `json:"address"`
				Wallet          string `json:"wallet"`
				OwnerAddress    string `json:"owner_address"`
				Contract        string `json:"contract"`
				ContractAddress string `json:"contract_address"`
				TokenID         string `json:"token_id"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
				return
			}
			wallet := req.Wallet
			if wallet == "" {
				if req.Address != "" {
					wallet = req.Address
				} else {
					wallet = req.OwnerAddress
				}
			}
			contract := req.Contract
			if contract == "" {
				contract = req.ContractAddress
			}
			chainID := req.ChainID
			if chainID == 0 {
				chainID = defaultChainID
			}

			var (
				hasNFT   bool
				balance  int64
				err      error
				cacheHit bool
			)

			cacheKey := nftCacheKey(chainID, wallet, contract, req.TokenID)
			if cache != nil {
				if cached, ok := cache.get(cacheKey); ok {
					hasNFT = cached.HasNFT
					balance = cached.Balance
					cacheHit = true
				}
			}
			if !cacheHit {
				if req.TokenID != "" {
					hasNFT, err = web3Service.VerifyNFTOwnership(c.Request.Context(), chainID, contract, req.TokenID, wallet)
					if hasNFT {
						balance = 1
					}
				} else {
					balance, err = web3Service.GetNFTBalance(c.Request.Context(), chainID, contract, wallet)
					hasNFT = balance > 0
				}
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if cache != nil && !cacheHit {
				cache.set(cacheKey, cachedNFTAccess{
					HasNFT:    hasNFT,
					Balance:   balance,
					ExpiresAt: time.Now().Add(60 * time.Second),
				})
			}
			c.JSON(http.StatusOK, gin.H{
				"has_nft":   hasNFT,
				"balance":   balance,
				"chain_id":  chainID,
				"contract":  contract,
				"cache_hit": cacheHit,
			})
		})
	}
	log.Info("NFT routes registered")
}

// registerStreamingRoutes registers streaming routes
func registerStreamingRoutes(router *gin.Engine, log *zap.Logger, web3Service nftAccessVerifier, authService *service.AuthService, defaultChainID int64, cache *nftAccessCache) {
	streaming := router.Group("/api/v1/streaming")
	{
		streaming.GET("/:id/manifest.m3u8", func(c *gin.Context) {
			authHeader := c.GetHeader("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			claims, err := authService.ParseToken(token)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}

			wallet := claims.WalletAddress
			if wallet == "" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "token missing wallet address"})
				return
			}

			contract := c.Query("contract")
			if contract == "" {
				contract = c.Query("contract_address")
			}
			if contract == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "contract is required"})
				return
			}

			chainID := defaultChainID
			if raw := c.Query("chain_id"); raw != "" {
				if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
					chainID = parsed
				}
			}
			tokenID := c.Query("token_id")

			var hasNFT bool
			cacheKey := nftCacheKey(chainID, wallet, contract, tokenID)
			if cache != nil {
				if cached, ok := cache.get(cacheKey); ok {
					hasNFT = cached.HasNFT
				} else if tokenID != "" {
					hasNFT, err = web3Service.VerifyNFTOwnership(c.Request.Context(), chainID, contract, tokenID, wallet)
					if err == nil {
						cache.set(cacheKey, cachedNFTAccess{HasNFT: hasNFT, Balance: 1, ExpiresAt: time.Now().Add(60 * time.Second)})
					}
				} else {
					balance, balanceErr := web3Service.GetNFTBalance(c.Request.Context(), chainID, contract, wallet)
					err = balanceErr
					hasNFT = balance > 0
					if err == nil {
						cache.set(cacheKey, cachedNFTAccess{HasNFT: hasNFT, Balance: balance, ExpiresAt: time.Now().Add(60 * time.Second)})
					}
				}
			} else if tokenID != "" {
				hasNFT, err = web3Service.VerifyNFTOwnership(c.Request.Context(), chainID, contract, tokenID, wallet)
			} else {
				balance, balanceErr := web3Service.GetNFTBalance(c.Request.Context(), chainID, contract, wallet)
				err = balanceErr
				hasNFT = balance > 0
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if !hasNFT {
				c.JSON(http.StatusForbidden, gin.H{"error": "nft access denied"})
				return
			}

			contentID := c.Param("id")
			playbackToken, err := authService.GeneratePlaybackToken(wallet, contentID, contract, tokenID, chainID, 2*time.Minute)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.String(http.StatusOK,
				"#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:10.0,\n/api/v1/streaming/%s/segment/0?playback_token=%s\n#EXT-X-ENDLIST\n",
				contentID, playbackToken,
			)
		})
		streaming.GET("/:id/segment/:num", func(c *gin.Context) {
			playbackToken := strings.TrimSpace(c.Query("playback_token"))
			if playbackToken == "" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "missing playback token"})
				return
			}
			if _, err := authService.ValidatePlaybackToken(playbackToken, c.Param("id")); err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid playback token"})
				return
			}
			c.Header("Content-Type", "video/mp2t")
			c.String(http.StatusOK, "segment")
		})
	}
	log.Info("Streaming routes registered")
}

// registerUploadRoutes registers upload routes
func registerUploadRoutes(router *gin.Engine, log *zap.Logger) {
	upload := router.Group("/api/v1/upload")
	{
		upload.POST("", func(c *gin.Context) {
			c.JSON(http.StatusCreated, gin.H{"message": "upload endpoint"})
		})
		upload.POST("/chunk", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "chunk upload endpoint"})
		})
		upload.GET("/:id/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "upload status endpoint"})
		})
	}
	log.Info("Upload routes registered")
}

// registerWeb3Routes registers web3 support routes.
func registerWeb3Routes(router *gin.Engine, log *zap.Logger, web3Service web3StatusProvider) {
	web3 := router.Group("/api/v1/web3")
	{
		web3.GET("/rpc-status", func(c *gin.Context) {
			statusesByChain := web3Service.GetRPCStatuses()
			chains := web3Service.GetSupportedChains()
			nameByChain := make(map[int64]string, len(chains))
			for _, chain := range chains {
				nameByChain[chain.ID] = chain.Name
			}

			response := make([]gin.H, 0, len(statusesByChain))
			for chainID, statuses := range statusesByChain {
				rpcs := make([]gin.H, 0, len(statuses))
				for _, status := range statuses {
					rpc := gin.H{
						"url":       status.URL,
						"is_active": status.IsActive,
						"failures":  status.Failures,
					}
					if !status.LastFailureAt.IsZero() {
						rpc["last_failure_at"] = status.LastFailureAt.Format(time.RFC3339)
					}
					if !status.CooldownUntil.IsZero() {
						rpc["cooldown_until"] = status.CooldownUntil.Format(time.RFC3339)
					}
					rpcs = append(rpcs, rpc)
				}
				response = append(response, gin.H{
					"chain_id": chainID,
					"name":     nameByChain[chainID],
					"rpcs":     rpcs,
				})
			}
			c.JSON(http.StatusOK, gin.H{"chains": response})
		})
	}
	log.Info("Web3 routes registered")
}

// registerTranscodingRoutes registers transcoding routes.
func registerTranscodingRoutes(router *gin.Engine, log *zap.Logger, handler *apiV1.TranscodingHandler) {
	transcode := router.Group("/api/v1/transcode")
	{
		transcode.POST("/submit", handler.Submit)
		transcode.GET("/status/:id", handler.GetStatus)
		transcode.POST("/cancel/:id", handler.Cancel)
		transcode.GET("/tasks", handler.ListTasks)
		transcode.GET("/profiles", handler.ListProfiles)
	}
	log.Info("Transcoding routes registered")
}
