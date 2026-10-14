package gateway

import (
	"errors"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/storage"
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

// TestAcceptance_ProductionDependencyFailure pins the SG-14 profile contract
// end to end at the provider level: under a strict profile (production,
// durable) an unreachable or unwired Redis/NATS means an ERROR, never an
// in-memory store; under demo the same failure degrades to memory with a
// logged warning naming the profile. The NATS dialer is injected so the test
// needs no live broker.
func TestAcceptance_ProductionDependencyFailure(t *testing.T) {
	// Redis-backed stores: production ⇒ error, demo ⇒ memory fallback.
	t.Run("redis stores", func(t *testing.T) {
		prod := productionGatewayConfig()

		store, err := provideChallengeStore(&RouterConfig{}, prod, zap.NewNop(), time.Minute, nil, &AppResources{})
		assert.Error(t, err, "production must not build an in-memory challenge store")
		assert.Nil(t, store)

		bl, err := provideTokenBlacklist(prod, zap.NewNop(), nil, &AppResources{})
		assert.Error(t, err, "production must not build an in-memory token blacklist")
		assert.Nil(t, bl)

		ss, err := provideSessionStore(prod, zap.NewNop(), nil)
		assert.Error(t, err, "production must not build an in-memory session store")
		assert.Nil(t, ss)

		demo := productionGatewayConfig()
		demo.Environment = "dev"

		store, err = provideChallengeStore(&RouterConfig{}, demo, zap.NewNop(), time.Minute, nil, &AppResources{})
		assert.NoError(t, err, "demo profile keeps the challenge-store fallback")
		assert.NotNil(t, store)

		bl, err = provideTokenBlacklist(demo, zap.NewNop(), nil, &AppResources{})
		assert.NoError(t, err, "demo profile keeps the blacklist fallback")
		assert.NotNil(t, bl)

		ss, err = provideSessionStore(demo, zap.NewNop(), nil)
		assert.NoError(t, err, "demo profile keeps the session-store fallback")
		assert.NotNil(t, ss)
	})

	// NATS queue: production ⇒ error, demo ⇒ in-memory queue. The dialer is
	// replaced with one that always fails, so the branch under test is the
	// profile decision, not broker connectivity.
	t.Run("nats queue", func(t *testing.T) {
		orig := natsQueueDialer
		t.Cleanup(func() { natsQueueDialer = orig })
		natsQueueDialer = func(string, *zap.Logger) (*storage.NATSTranscodingQueue, error) {
			return nil, errors.New("injected: nats unreachable")
		}

		prod := productionGatewayConfig()
		svc, err := provideTranscodingService(prod, zap.NewNop(), nil, nil, &AppResources{})
		assert.Error(t, err, "production must not fall back to an in-memory transcoding queue")
		assert.Nil(t, svc)
		assert.Contains(t, err.Error(), "nats")

		demo := productionGatewayConfig()
		demo.Environment = "dev"
		svc, err = provideTranscodingService(demo, zap.NewNop(), nil, nil, &AppResources{})
		assert.NoError(t, err, "demo profile keeps the in-memory queue fallback")
		assert.NotNil(t, svc)
		if svc != nil {
			svc.StopWorker()
		}
	})

	// durable profile behaves like production for dependencies: it is the
	// acceptance profile and refuses memory fallback too.
	t.Run("durable profile is strict", func(t *testing.T) {
		durable := productionGatewayConfig()
		durable.Environment = "dev" // not "production" — the explicit profile decides
		durable.Profile = config.ProfileDurable

		_, err := provideRedis(durable, zap.NewNop(), &AppResources{})
		assert.Error(t, err, "durable profile must not fall back to memory when Redis is unreachable")

		store, err := provideChallengeStore(&RouterConfig{}, durable, zap.NewNop(), time.Minute, nil, &AppResources{})
		assert.Error(t, err)
		assert.Nil(t, store)
	})
}
