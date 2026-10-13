package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// productionConfigBase returns a config that satisfies every other production
// requirement, so each case below can knock out exactly one dependency.
func productionConfigBase() *Config {
	cfg := DefaultConfig()
	cfg.Auth.JWTSecret = "a-real-production-secret-32chars!!"
	cfg.Database.Host = "db.example.internal"
	cfg.Database.Password = "a-secure-production-pw"
	cfg.Database.SSLMode = "require"
	cfg.Redis.Host = "redis.example.internal"
	cfg.Redis.Password = "redis-pw"
	cfg.NATS.URL = "nats://nats.example.internal:4222"
	cfg.Storage.AccessKey = "real-key"
	cfg.Storage.SecretKey = "real-secret"
	cfg.Storage.UseSSL = true
	cfg.Web3.EthereumRPC = "https://mainnet.infura.io/v3/real-key"
	return cfg
}

func TestValidateProduction_RequiresDependenciesInProd(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"redis addr empty", func(c *Config) { c.Redis.Host = "" }, "redis"},
		{"nats url empty", func(c *Config) { c.NATS.URL = "" }, "nats"},
		{"database host empty", func(c *Config) { c.Database.Host = "" }, "database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := productionConfigBase()
			tt.mutate(cfg)

			err := cfg.ValidateProduction(zap.NewNop())
			assert.Error(t, err, "a production config without %s must not validate", tt.wantSub)
			if err == nil {
				return
			}
			ve, ok := err.(*ValidationError)
			assert.True(t, ok, "must be a ValidationError, got %T", err)
			if ok {
				assert.True(t, ve.HasCritical(), "missing dependency must be CRITICAL, not a warning")
			}
			assert.Contains(t, err.Error(), tt.wantSub)
		})
	}

	t.Run("all dependencies present passes", func(t *testing.T) {
		cfg := productionConfigBase()
		assert.NoError(t, cfg.ValidateProduction(zap.NewNop()))
	})
}

func TestValidateProduction_RegistryConfiguredMustBeComplete(t *testing.T) {
	t.Run("registry enabled without signer key rejected", func(t *testing.T) {
		cfg := productionConfigBase()
		cfg.Web3.ContentRegistryAddress = "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"
		cfg.Web3.AnvilDeployerKey = ""

		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err, "registry address without a signer key must be refused")
		if err != nil {
			assert.Contains(t, err.Error(), "content registry")
		}
	})

	t.Run("registry enabled with zero chain id rejected", func(t *testing.T) {
		cfg := productionConfigBase()
		cfg.Web3.ContentRegistryAddress = "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"
		cfg.Web3.AnvilDeployerKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
		cfg.Web3.ChainID = 0

		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err, "registry address with chain id 0 must be refused")
		if err != nil {
			assert.Contains(t, err.Error(), "content registry")
		}
	})

	t.Run("registry enabled without rpc rejected", func(t *testing.T) {
		cfg := productionConfigBase()
		cfg.Web3.ContentRegistryAddress = "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"
		cfg.Web3.AnvilDeployerKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
		cfg.Web3.EthereumRPC = ""

		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err, "registry enabled without an RPC endpoint must be refused")
		if err != nil {
			assert.Contains(t, err.Error(), "content registry")
		}
	})

	t.Run("registry disabled by empty address passes", func(t *testing.T) {
		cfg := productionConfigBase()
		cfg.Web3.ContentRegistryAddress = ""
		assert.NoError(t, cfg.ValidateProduction(zap.NewNop()))
	})

	t.Run("complete registry config passes", func(t *testing.T) {
		cfg := productionConfigBase()
		cfg.Web3.ContentRegistryAddress = "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"
		cfg.Web3.AnvilDeployerKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
		assert.NoError(t, cfg.ValidateProduction(zap.NewNop()))
	})
}

func TestIsProduction(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"production", true},
		{"prod", true},
		{"Production", true},
		{"dev", false},
		{"development", false},
		{"test", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			cfg := &Config{Environment: tt.env}
			assert.Equal(t, tt.want, cfg.IsProduction())
		})
	}
}
