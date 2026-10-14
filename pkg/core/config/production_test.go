package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDeriveProfile(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		debug       bool
		explicit    string
		want        string
	}{
		{"production env, no debug", "production", false, "", ProfileProduction},
		{"prod env, no debug", "prod", false, "", ProfileProduction},
		{"production env, debug on ⇒ demo", "production", true, "", ProfileDemo},
		{"debug with any env ⇒ demo", "dev", true, "", ProfileDemo},
		{"test env ⇒ unit", "test", false, "", ProfileUnit},
		{"unit env ⇒ unit", "unit", true, "", ProfileUnit},
		{"dev env, no debug ⇒ demo", "dev", false, "", ProfileDemo},
		{"explicit wins over everything", "production", false, ProfileDurable, ProfileDurable},
		{"explicit demo wins over production env", "production", false, ProfileDemo, ProfileDemo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DeriveProfile(tt.environment, tt.debug, tt.explicit))
		})
	}

	t.Run("EffectiveProfile derives when the field is empty", func(t *testing.T) {
		cfg := &Config{Environment: "production"}
		assert.Equal(t, ProfileProduction, cfg.EffectiveProfile())
		cfg.Profile = ProfileDurable
		assert.Equal(t, ProfileDurable, cfg.EffectiveProfile(), "an explicit profile wins")
	})
}

// productionKeyPolicyConfig is a fully valid production config, so each case
// below can knock out exactly one key/secrets rule.
func productionKeyPolicyConfig() *Config {
	cfg := productionConfigBase()
	cfg.Environment = "production"
	return cfg
}

// TestValidateProduction_ProductionSecretsRejected pins the production-only
// key policy (SG-14 step 4): published demo/test secrets and the well-known
// Anvil key are CRITICAL under the production profile, but only WARNINGS
// under demo/durable — the compose acceptance stack (APP_DEBUG=true) runs on
// exactly these values and must keep starting.
func TestValidateProduction_ProductionSecretsRejected(t *testing.T) {
	t.Run("production: compose acceptance JWT secret is critical", func(t *testing.T) {
		cfg := productionKeyPolicyConfig()
		cfg.Auth.JWTSecret = "fullchain-acceptance-secret-2026"

		err := cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		ve := err.(*ValidationError)
		assert.True(t, ve.HasCritical(), "a published compose secret must be CRITICAL in production")
		assert.Contains(t, err.Error(), "fullchain-acceptance-secret-2026")
	})

	t.Run("production: dev placeholder JWT secret is critical", func(t *testing.T) {
		cfg := productionKeyPolicyConfig()
		cfg.Auth.JWTSecret = "jwt-secret-placeholder-set-STREAMGATE_JWT_SECRET-env"

		err := cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		assert.True(t, err.(*ValidationError).HasCritical(),
			"the dev placeholder secret must be CRITICAL in production")
	})

	t.Run("production: well-known Anvil tx key is critical", func(t *testing.T) {
		cfg := productionKeyPolicyConfig()
		cfg.Web3.Transaction.PrivateKeyHex = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

		err := cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		assert.True(t, err.(*ValidationError).HasCritical(),
			"the public Anvil/Hardhat test key must be CRITICAL in production")
		assert.Contains(t, err.Error(), "well-known Anvil/Hardhat test key")
	})

	t.Run("production: anvil deployer key set is critical", func(t *testing.T) {
		cfg := productionKeyPolicyConfig()
		cfg.Web3.AnvilDeployerKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

		err := cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		assert.True(t, err.(*ValidationError).HasCritical(),
			"the dev deployer key must not exist in a production deployment")
		assert.Contains(t, err.Error(), "anvil_deployer_key")
	})

	t.Run("demo profile: the same values are warnings, not failures", func(t *testing.T) {
		// Mirrors the compose acceptance stack: APP_DEBUG=true derives the demo
		// profile with exactly these values, so ValidateProduction must report
		// them as warnings the mains log — never as a startup failure.
		cfg := productionKeyPolicyConfig()
		cfg.Debug = true
		cfg.Auth.JWTSecret = "fullchain-acceptance-secret-2026"
		cfg.Web3.AnvilDeployerKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
		cfg.Web3.Transaction.PrivateKeyHex = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

		err := cfg.ValidateProduction(zap.NewNop())
		if err != nil {
			ve := err.(*ValidationError)
			assert.False(t, ve.HasCritical(),
				"demo profile must not turn the acceptance stack's own values into criticals")
			assert.NotEmpty(t, ve.Warnings, "the swallowed findings must still be reported as warnings")
		}
	})

	t.Run("rotation list bounded at 2", func(t *testing.T) {
		cfg := productionKeyPolicyConfig()
		cfg.Auth.PreviousAccessSecrets = []string{"a", "b", "c"}

		err := cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		assert.True(t, err.(*ValidationError).HasCritical(),
			"more than MaxPreviousSecrets previous access secrets must be refused")
		assert.Contains(t, err.Error(), "previous_access_secrets")

		cfg.Auth.PreviousAccessSecrets = []string{"a", "b"}
		cfg.Auth.PreviousCapabilitySecrets = []string{"a", "b", "c", "d"}
		err = cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "previous_capability_secrets")

		cfg.Auth.PreviousCapabilitySecrets = []string{"a", "b"}
		assert.NoError(t, cfg.ValidateProduction(zap.NewNop()), "exactly 2 previous secrets must be allowed")
	})

	t.Run("explicit short derived keys are critical", func(t *testing.T) {
		cfg := productionKeyPolicyConfig()
		cfg.Auth.AccessSecret = "short-access"

		err := cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "access_secret")

		cfg.Auth.AccessSecret = ""
		cfg.Auth.CapabilitySecret = "short-cap"
		err = cfg.ValidateProduction(zap.NewNop())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "capability_secret")
	})
}
