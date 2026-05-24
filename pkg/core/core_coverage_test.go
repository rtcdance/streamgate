package core

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewApp_CreatesApp(t *testing.T) {
	logger := zap.NewNop()
	cfg := AppConfig{
		Logger: logger,
	}
	app := NewApp(cfg)
	assert.NotNil(t, app)
	assert.NotNil(t, app.life)
	assert.Equal(t, logger, app.logger)
}

func TestApp_RegisterStop_NilHTTPServer(t *testing.T) {
	app := NewApp(AppConfig{Logger: zap.NewNop()})
	err := app.registerStop(context.Background())
	assert.NoError(t, err)
}

func TestApp_RegisterStop_WithHTTPServer(t *testing.T) {
	srv := &http.Server{}
	app := NewApp(AppConfig{
		HTTPServer: srv,
		Logger:     zap.NewNop(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := app.registerStop(ctx)
	assert.NoError(t, err)
}

func TestApp_RegisterMiddleware_NilGinEngine(t *testing.T) {
	app := NewApp(AppConfig{Logger: zap.NewNop()})
	err := app.registerMiddleware(context.Background())
	assert.NoError(t, err)
}

func TestApp_RegisterMiddleware_WithJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	called := false
	jwtMiddleware := func(c *gin.Context) {
		called = true
		c.Next()
	}

	app := NewApp(AppConfig{
		GinEngine:         r,
		JWTAuthMiddleware: jwtMiddleware,
		Logger:            zap.NewNop(),
	})

	err := app.registerMiddleware(context.Background())
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestApp_RegisterMiddleware_WithNFTGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	called := false
	nftMiddleware := func(c *gin.Context) {
		called = true
		c.Next()
	}

	app := NewApp(AppConfig{
		GinEngine:         r,
		NFTGateMiddleware: nftMiddleware,
		Logger:            zap.NewNop(),
	})

	err := app.registerMiddleware(context.Background())
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestApp_RegisterMiddleware_BothMiddlewares(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	jwtCalled := false
	nftCalled := false

	app := NewApp(AppConfig{
		GinEngine: r,
		JWTAuthMiddleware: func(c *gin.Context) {
			jwtCalled = true
			c.Next()
		},
		NFTGateMiddleware: func(c *gin.Context) {
			nftCalled = true
			c.Next()
		},
		Logger: zap.NewNop(),
	})

	err := app.registerMiddleware(context.Background())
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(w, req)
	assert.True(t, jwtCalled)
	assert.True(t, nftCalled)
}

func TestApp_StartServer_NilHTTPServer(t *testing.T) {
	app := NewApp(AppConfig{Logger: zap.NewNop()})
	err := app.startServer(context.Background())
	assert.NoError(t, err)
}

func TestApp_StartServer_WithServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	srv := &http.Server{}
	app := NewApp(AppConfig{
		HTTPServer: srv,
		GinEngine:  r,
		ListenAddr: ":0",
		Logger:     zap.NewNop(),
	})

	ctx := context.Background()
	err := app.startServer(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func TestApp_StartServer_DefaultAddr(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	srv := &http.Server{}
	app := NewApp(AppConfig{
		HTTPServer: srv,
		GinEngine:  r,
		Logger:     zap.NewNop(),
	})

	ctx := context.Background()
	err := app.startServer(ctx)
	require.NoError(t, err)
	assert.Equal(t, ":8080", srv.Addr)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func TestApp_Start_NilHTTPServer(t *testing.T) {
	app := NewApp(AppConfig{Logger: zap.NewNop()})
	err := app.Start(context.Background())
	assert.NoError(t, err)
}

func TestApp_Start_WithServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	srv := &http.Server{}
	app := NewApp(AppConfig{
		HTTPServer: srv,
		GinEngine:  r,
		ListenAddr: ":0",
		Logger:     zap.NewNop(),
	})

	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func TestWireHelper_Defaults(t *testing.T) {
	cfg := WireHelper(nil, nil, nil, 1, 0, "https://example.com")
	assert.Equal(t, 60*time.Second, cfg.CacheTTL)
	assert.Equal(t, int64(1), cfg.DefaultChainID)
	assert.Equal(t, "https://example.com", cfg.MarketplaceURL)
	assert.Nil(t, cfg.Verifier)
	assert.Nil(t, cfg.BlockProver)
	assert.Nil(t, cfg.Cache)
}

func TestWireHelper_CustomCacheTTL(t *testing.T) {
	cfg := WireHelper(nil, nil, nil, 137, 30*time.Second, "https://opensea.io")
	assert.Equal(t, 30*time.Second, cfg.CacheTTL)
	assert.Equal(t, int64(137), cfg.DefaultChainID)
}

func TestIsDraining_Initial(t *testing.T) {
	drainState.Store(false)
	assert.False(t, IsDraining())
}

func TestSetDraining(t *testing.T) {
	drainState.Store(false)
	SetDraining()
	assert.True(t, IsDraining())
	drainState.Store(false)
}

func TestDrainMiddleware_NotDraining(t *testing.T) {
	drainState.Store(false)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(DrainMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDrainMiddleware_Draining(t *testing.T) {
	drainState.Store(true)
	defer drainState.Store(false)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(DrainMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestDrainMiddleware_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		draining   bool
		wantStatus int
	}{
		{"not draining", false, http.StatusOK},
		{"draining", true, http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drainState.Store(tt.draining)
			defer drainState.Store(false)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(DrainMiddleware())
			r.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestMicrokernel_RegisterPlugin_NilName(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "", version: "1.0.0"}
	err := kernel.RegisterPlugin(p)
	assert.NoError(t, err)
}

func TestMicrokernel_Health_NotStarted(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "test", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p))

	err := kernel.Health(context.Background())
	assert.NoError(t, err)
}

func TestMicrokernel_Start_PluginWithNoDeps(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "nodep", version: "1.0.0", deps: nil}
	require.NoError(t, kernel.RegisterPlugin(p))

	err := kernel.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, p.initialized)
	assert.True(t, p.started)
}

func TestMicrokernel_Start_EmptyDeps(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "emptydeps", version: "1.0.0", deps: []string{}}
	require.NoError(t, kernel.RegisterPlugin(p))

	err := kernel.Start(context.Background())
	require.NoError(t, err)
}

func TestMicrokernel_GetConfig_NilConfig(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)
	retrieved := kernel.GetConfig()
	assert.Equal(t, cfg, retrieved)
}

func TestMicrokernel_LoadRegisteredPlugins_NoFactories(t *testing.T) {
	factoryMu.Lock()
	for k := range pluginFactories {
		delete(pluginFactories, k)
	}
	factoryMu.Unlock()

	kernel := newTestKernel(t)
	err := kernel.LoadRegisteredPlugins()
	assert.NoError(t, err)
}

func TestMicrokernel_Shutdown_AlreadyStopped(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "test", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p))
	require.NoError(t, kernel.Start(context.Background()))

	err := kernel.Shutdown(context.Background())
	require.NoError(t, err)

	err = kernel.Shutdown(context.Background())
	assert.Error(t, err)
}

func TestAppConfig_Fields(t *testing.T) {
	cfg := AppConfig{
		HTTPServer:        &http.Server{},
		GinEngine:         gin.New(),
		ListenAddr:        ":9090",
		JWTAuthMiddleware: func(c *gin.Context) { c.Next() },
		NFTGateMiddleware: func(c *gin.Context) { c.Next() },
		Logger:            zap.NewNop(),
	}
	assert.NotNil(t, cfg.HTTPServer)
	assert.NotNil(t, cfg.GinEngine)
	assert.Equal(t, ":9090", cfg.ListenAddr)
	assert.NotNil(t, cfg.JWTAuthMiddleware)
	assert.NotNil(t, cfg.NFTGateMiddleware)
	assert.NotNil(t, cfg.Logger)
}

func TestWireHelper_WithAllFields(t *testing.T) {
	verifier := &coreCovMockNFTVerifier{}
	blockProver := &coreCovMockBlockProver{}
	cache := &coreCovMockCache{}

	cfg := WireHelper(verifier, blockProver, cache, 1, 45*time.Second, "https://market.io")
	assert.Equal(t, verifier, cfg.Verifier)
	assert.Equal(t, blockProver, cfg.BlockProver)
	assert.Equal(t, cache, cfg.Cache)
	assert.Equal(t, int64(1), cfg.DefaultChainID)
	assert.Equal(t, 45*time.Second, cfg.CacheTTL)
	assert.Equal(t, "https://market.io", cfg.MarketplaceURL)
}

type coreCovMockNFTVerifier struct{}

func (m *coreCovMockNFTVerifier) VerifyNFTOwnership(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return true, nil
}
func (m *coreCovMockNFTVerifier) VerifyNFTOwnershipAutoDetect(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return true, nil
}
func (m *coreCovMockNFTVerifier) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, _, _ string) (bool, error) {
	return true, nil
}
func (m *coreCovMockNFTVerifier) GetNFTBalance(_ context.Context, _ int64, _, _ string) (*big.Int, error) {
	return big.NewInt(1), nil
}
func (m *coreCovMockNFTVerifier) GetNFTInfo(_ context.Context, _ int64, _, _ string) (*middleware.NFTMetadata, error) {
	return nil, nil
}

type coreCovMockBlockProver struct{}

func (m *coreCovMockBlockProver) HeaderByNumber(_ context.Context, _ *big.Int) (*middleware.BlockHeaderInfo, error) {
	return &middleware.BlockHeaderInfo{Number: 1}, nil
}

type coreCovMockCache struct{}

func (m *coreCovMockCache) Get(_ context.Context, _ string) (middleware.NFTAccessEntry, bool) {
	return middleware.NFTAccessEntry{}, false
}
func (m *coreCovMockCache) Set(_ context.Context, _ string, _ middleware.NFTAccessEntry) {}
func (m *coreCovMockCache) Delete(_ context.Context, _ string)                            {}
func (m *coreCovMockCache) DeleteByPrefix(_ context.Context, _ string)                    {}
