package gateway

import (
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// productionGatewayConfig returns a config in the production profile with
// dependency addresses that cannot reach anything real, so a fail-fast path
// must reject them instead of silently wiring in-memory fallbacks.
func productionGatewayConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Environment = "production"
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 1 // closed port: Redis is unreachable
	cfg.NATS.URL = ""  // no durable queue configured
	cfg.Database.Host = "127.0.0.1"
	cfg.Database.Port = 1 // closed port: PostgreSQL is unreachable
	return cfg
}

func TestBuildProviders_ProductionWithoutRedisFailsFast(t *testing.T) {
	cfg := productionGatewayConfig()

	_, err := provideRedis(cfg, zap.NewNop(), &AppResources{})
	assert.Error(t, err, "production must not fall back to in-memory stores when Redis is unreachable")
	if err != nil {
		assert.Contains(t, err.Error(), "redis")
	}
}

// The production path must refuse BEFORE dialling anything: a missing address
// is a configuration error, not a connectivity one.
func TestBuildProviders_ProductionWithoutRedisAddrFailsFast(t *testing.T) {
	cfg := productionGatewayConfig()
	cfg.Redis.Host = ""

	_, err := provideRedis(cfg, zap.NewNop(), &AppResources{})
	assert.Error(t, err, "an empty redis address must not become an in-memory fallback in production")
	if err != nil {
		assert.Contains(t, err.Error(), "redis")
	}
}

func TestBuildProviders_ProductionWithoutDatabaseFailsFast(t *testing.T) {
	t.Run("unreachable", func(t *testing.T) {
		cfg := productionGatewayConfig()

		db, sqlDB, err := provideDatabase(cfg, zap.NewNop(), &AppResources{})
		assert.Error(t, err, "production must not fall back to in-memory when PostgreSQL is unreachable")
		assert.Nil(t, db)
		assert.Nil(t, sqlDB)
	})

	t.Run("address empty", func(t *testing.T) {
		cfg := productionGatewayConfig()
		cfg.Database.Host = ""

		db, sqlDB, err := provideDatabase(cfg, zap.NewNop(), &AppResources{})
		assert.Error(t, err, "an empty database host must not become an in-memory fallback")
		assert.Nil(t, db)
		assert.Nil(t, sqlDB)
	})
}

func TestBuildProviders_ProductionWithoutNATSFailsFast(t *testing.T) {
	cfg := productionGatewayConfig()

	svc, err := provideTranscodingService(cfg, zap.NewNop(), nil, nil, &AppResources{})
	assert.Error(t, err, "production must not fall back to an in-memory transcoding queue")
	assert.Nil(t, svc, "production must not build a transcoding service on an in-memory queue")
	if err != nil {
		assert.Contains(t, err.Error(), "nats")
	}
}

// SetupRouter is the composition root both entrypoints use; in production a
// missing durable dependency must surface as an error so the process exits
// non-zero rather than starting degraded.
func TestSetupRouter_ProductionWithoutDepsFailsFast(t *testing.T) {
	_, _, err := SetupRouter(productionGatewayConfig(), zap.NewNop())
	assert.Error(t, err, "SetupRouter must fail in production when Redis is unreachable")
	if err != nil {
		assert.Contains(t, err.Error(), "redis")
	}
}

// Dev/demo behaviour must be preserved: unreachable Redis still degrades to
// in-memory stores when the config is not in the production profile.
func TestBuildProviders_DevStillFallsBackToMemory(t *testing.T) {
	cfg := productionGatewayConfig()
	cfg.Environment = "dev"

	redisClient, err := provideRedis(cfg, zap.NewNop(), &AppResources{})
	assert.NoError(t, err, "dev profile keeps the memory fallback")
	assert.Nil(t, redisClient)
}
