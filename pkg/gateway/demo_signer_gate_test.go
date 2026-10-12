package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// The server-side signer is the one place the platform spends a key that is not
// the user's. These tests pin the three-way gate that decides whether that
// surface exists at all.

func TestDemoSignerAllowed(t *testing.T) {
	allowed := func() *config.Config {
		cfg := config.DefaultConfig()
		cfg.Debug = true
		cfg.Web3.ChainID = AnvilChainID
		cfg.Features.DemoTxSigner = true
		return cfg
	}

	t.Run("all three conditions met", func(t *testing.T) {
		assert.True(t, DemoSignerAllowed(allowed()))
	})

	// Each condition alone must be enough to refuse: a single missing check is
	// how a production deployment would end up exposing a signing oracle.
	t.Run("production (debug off) is refused", func(t *testing.T) {
		cfg := allowed()
		cfg.Debug = false
		assert.False(t, DemoSignerAllowed(cfg), "production must never register the signer")
	})

	t.Run("non-anvil chain is refused", func(t *testing.T) {
		cfg := allowed()
		cfg.Web3.ChainID = 11155111
		assert.False(t, DemoSignerAllowed(cfg), "only the local anvil chain may sign")
	})

	t.Run("without the explicit opt-in flag", func(t *testing.T) {
		cfg := allowed()
		cfg.Features.DemoTxSigner = false
		assert.False(t, DemoSignerAllowed(cfg), "the flag must be an explicit opt-in")
	})

	t.Run("nil config", func(t *testing.T) {
		assert.False(t, DemoSignerAllowed(nil))
	})
}

func demoMintRouteRegistered(r *gin.Engine) bool {
	for _, rt := range r.Routes() {
		if rt.Method == http.MethodPost && rt.Path == APIPrefix+"/nft/dev/mint" {
			return true
		}
	}
	return false
}

func TestRegisterNFTDevMintRoute_OnlyWhenAllowed(t *testing.T) {
	// A zero-value minter is enough: the gate decides registration, and the
	// handler is never invoked in this test.
	minter := &service.DemoNFTMinter{}

	gin.SetMode(gin.TestMode)
	denied := gin.New()
	RegisterNFTDevMintRoute(denied, minter, zap.NewNop(), false)
	assert.False(t, demoMintRouteRegistered(denied), "route must not exist without the opt-in")

	permitted := gin.New()
	RegisterNFTDevMintRoute(permitted, minter, zap.NewNop(), true)
	assert.True(t, demoMintRouteRegistered(permitted), "route must exist when explicitly allowed")
}

// A disallowed deployment must answer 404 — not 401/403, which would advertise
// that a signer exists behind an auth wall.
func TestRegisterNFTDevMintRoute_NotAllowedIsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterNFTDevMintRoute(r, &service.DemoNFTMinter{}, zap.NewNop(), false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/dev/mint",
		strings.NewReader(`{"count":1}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Without a configured minter the route is absent even when the flag is on.
func TestRegisterNFTDevMintRoute_NilMinter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterNFTDevMintRoute(r, nil, zap.NewNop(), true)
	assert.False(t, demoMintRouteRegistered(r))
}
