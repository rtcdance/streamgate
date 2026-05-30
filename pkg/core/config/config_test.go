package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSetDefaults(t *testing.T) {
	t.Run("set default values", func(t *testing.T) {
		setDefaults()

		assert.Equal(t, "streamgate", viper.GetString("app.name"))
		assert.Equal(t, "monolith", viper.GetString("app.mode"))
		assert.Equal(t, false, viper.GetBool("app.debug"))
		assert.Equal(t, 8080, viper.GetInt("server.port"))
		assert.Equal(t, 30, viper.GetInt("server.read_timeout"))
		assert.Equal(t, 30, viper.GetInt("server.write_timeout"))
		assert.Equal(t, 9090, viper.GetInt("grpc.port"))
		assert.Equal(t, "localhost", viper.GetString("consul.address"))
		assert.Equal(t, 8500, viper.GetInt("consul.port"))
		assert.Equal(t, "localhost", viper.GetString("database.host"))
		assert.Equal(t, 5432, viper.GetInt("database.port"))
		assert.Equal(t, "postgres", viper.GetString("database.user"))
		assert.Equal(t, "postgres", viper.GetString("database.password"))
		assert.Equal(t, "streamgate", viper.GetString("database.database"))
		assert.Equal(t, "disable", viper.GetString("database.sslmode"))
		assert.Equal(t, 100, viper.GetInt("database.maxconns"))
		assert.Equal(t, "localhost", viper.GetString("redis.host"))
		assert.Equal(t, 6379, viper.GetInt("redis.port"))
		assert.Equal(t, "", viper.GetString("redis.password"))
		assert.Equal(t, 0, viper.GetInt("redis.db"))
		assert.Equal(t, 100, viper.GetInt("redis.poolsize"))
		assert.Equal(t, "minio", viper.GetString("storage.type"))
		assert.Equal(t, "localhost:9000", viper.GetString("storage.endpoint"))
		assert.Equal(t, "minioadmin", viper.GetString("storage.accesskey"))
		assert.Equal(t, "minioadmin", viper.GetString("storage.secretkey"))
		assert.Equal(t, "streamgate", viper.GetString("storage.bucket"))
		assert.Equal(t, "us-east-1", viper.GetString("storage.region"))
		assert.Equal(t, "nats://localhost:4222", viper.GetString("nats.url"))
		assert.Equal(t, "https://sepolia.infura.io/v3/YOUR_KEY", viper.GetString("web3.ethereum_rpc"))
		assert.Equal(t, "https://api.devnet.solana.com", viper.GetString("web3.solana_rpc"))
		assert.Equal(t, int64(11155111), viper.GetInt64("web3.chain_id"))
		assert.Equal(t, 9090, viper.GetInt("monitoring.prometheus_port"))
		assert.Equal(t, "localhost:4317", viper.GetString("monitoring.jaeger_endpoint"))
		assert.Equal(t, "info", viper.GetString("monitoring.log_level"))
		assert.Empty(t, viper.GetStringSlice("plugins.enabled"))
	})
}

func TestDatabaseConfig_GetDSN(t *testing.T) {
	tests := []struct {
		name     string
		config   DatabaseConfig
		expected string
	}{
		{
			name: "basic config",
			config: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Database: "dbname",
				SSLMode:  "disable",
			},
			expected: "postgres://user:pass@localhost:5432/dbname?sslmode=disable",
		},
		{
			name: "with ssl",
			config: DatabaseConfig{
				Host:     "db.example.com",
				Port:     5433,
				User:     "admin",
				Password: "test-secret-that-is-at-least-32-chars",
				Database: "production",
				SSLMode:  "require",
			},
			expected: "postgres://admin:test-secret-that-is-at-least-32-chars@db.example.com:5433/production?sslmode=require",
		},
		{
			name: "special chars in password",
			config: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "admin",
				Password: "p@ss:w0rd",
				Database: "mydb",
				SSLMode:  "disable",
			},
			expected: "postgres://admin:p@ss:w0rd@localhost:5432/mydb?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetDSN()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("load config with defaults", func(t *testing.T) {
		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, "streamgate", cfg.AppName)
		assert.Equal(t, "monolith", cfg.Mode)
		assert.Equal(t, 8080, cfg.Server.Port)
		assert.Equal(t, false, cfg.Debug)
	})

	t.Run("load config with environment variables", func(t *testing.T) {
		_ = os.Setenv("STREAMGATE_DB_HOST", "custom-host")
		_ = os.Setenv("STREAMGATE_DB_PORT", "5433")
		_ = os.Setenv("STREAMGATE_DB_USER", "custom-user")
		_ = os.Setenv("STREAMGATE_DB_PASSWORD", "custom-pass")
		_ = os.Setenv("STREAMGATE_DB_NAME", "custom-db")
		_ = os.Setenv("STREAMGATE_DB_SSLMODE", "require")
		_ = os.Setenv("STREAMGATE_DB_MAXCONNS", "50")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, "custom-host", cfg.Database.Host)
		assert.Equal(t, 5433, cfg.Database.Port)
		assert.Equal(t, "custom-user", cfg.Database.User)
		assert.Equal(t, "custom-pass", cfg.Database.Password)
		assert.Equal(t, "custom-db", cfg.Database.Database)
		assert.Equal(t, "require", cfg.Database.SSLMode)
		assert.Equal(t, 50, cfg.Database.MaxConns)

		_ = os.Unsetenv("STREAMGATE_DB_HOST")
		_ = os.Unsetenv("STREAMGATE_DB_PORT")
		_ = os.Unsetenv("STREAMGATE_DB_USER")
		_ = os.Unsetenv("STREAMGATE_DB_PASSWORD")
		_ = os.Unsetenv("STREAMGATE_DB_NAME")
		_ = os.Unsetenv("STREAMGATE_DB_SSLMODE")
		_ = os.Unsetenv("STREAMGATE_DB_MAXCONNS")
	})

	t.Run("load config with redis env vars", func(t *testing.T) {
		_ = os.Setenv("STREAMGATE_REDIS_HOST", "redis-host")
		_ = os.Setenv("STREAMGATE_REDIS_PORT", "6380")
		_ = os.Setenv("STREAMGATE_REDIS_PASSWORD", "redis-pass")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "redis-host", cfg.Redis.Host)
		assert.Equal(t, 6380, cfg.Redis.Port)
		assert.Equal(t, "redis-pass", cfg.Redis.Password)

		_ = os.Unsetenv("STREAMGATE_REDIS_HOST")
		_ = os.Unsetenv("STREAMGATE_REDIS_PORT")
		_ = os.Unsetenv("STREAMGATE_REDIS_PASSWORD")
	})

	t.Run("load config with storage env vars", func(t *testing.T) {
		_ = os.Setenv("STREAMGATE_STORAGE_TYPE", "s3")
		_ = os.Setenv("STREAMGATE_STORAGE_ENDPOINT", "s3.amazonaws.com")
		_ = os.Setenv("STREAMGATE_STORAGE_BUCKET", "my-bucket")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "s3", cfg.Storage.Type)
		assert.Equal(t, "s3.amazonaws.com", cfg.Storage.Endpoint)
		assert.Equal(t, "my-bucket", cfg.Storage.Bucket)

		_ = os.Unsetenv("STREAMGATE_STORAGE_TYPE")
		_ = os.Unsetenv("STREAMGATE_STORAGE_ENDPOINT")
		_ = os.Unsetenv("STREAMGATE_STORAGE_BUCKET")
	})

	t.Run("load config with web3 env vars", func(t *testing.T) {
		_ = os.Setenv("STREAMGATE_ETH_RPC", "https://mainnet.infura.io/v3/test-key")
		_ = os.Setenv("STREAMGATE_SOLANA_RPC", "https://api.mainnet-beta.solana.com")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "https://mainnet.infura.io/v3/test-key", cfg.Web3.EthereumRPC)
		assert.Equal(t, "https://api.mainnet-beta.solana.com", cfg.Web3.SolanaRPC)

		_ = os.Unsetenv("STREAMGATE_ETH_RPC")
		_ = os.Unsetenv("STREAMGATE_SOLANA_RPC")
	})

	t.Run("load config with auth env vars", func(t *testing.T) {
		_ = os.Setenv("STREAMGATE_JWT_SECRET", "my-super-secret-jwt-key-32chars!!")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "my-super-secret-jwt-key-32chars!!", cfg.Auth.JWTSecret)

		_ = os.Unsetenv("STREAMGATE_JWT_SECRET")
	})

	t.Run("load config with nats env vars", func(t *testing.T) {
		_ = os.Setenv("STREAMGATE_NATS_URL", "nats://nats-host:4222")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "nats://nats-host:4222", cfg.NATS.URL)

		_ = os.Unsetenv("STREAMGATE_NATS_URL")
	})

	t.Run("load config with comma-separated CORS allowed origins env var", func(t *testing.T) {
		_ = os.Setenv("STREAMGATE_CORS_ORIGINS", "http://localhost:18000,http://localhost:3000,http://localhost:8080,http://localhost:18080")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, []string{
			"http://localhost:18000",
			"http://localhost:3000",
			"http://localhost:8080",
			"http://localhost:18080",
		}, cfg.CORS.AllowedOrigins)

		_ = os.Unsetenv("STREAMGATE_CORS_ORIGINS")
	})
}

func TestLoadConfig_InvalidServerPort(t *testing.T) {
	origViper := viper.GetViper()
	defer func() {
		viper.Reset()
		_ = origViper
	}()

	viper.Set("server.port", 0)
	setDefaults()
	viper.Set("server.port", 0)

	_, err := LoadConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid server port")
}

func TestLoadConfig_InvalidGRPCPort(t *testing.T) {
	viper.Set("grpc.port", -1)
	defer viper.Reset()

	_, err := LoadConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid gRPC port")
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "streamgate", cfg.AppName)
	assert.Equal(t, "monolith", cfg.Mode)
	assert.Equal(t, 8080, cfg.Port)
	assert.False(t, cfg.Debug)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, 30, cfg.Server.ReadTimeout)
	assert.Equal(t, 30, cfg.Server.WriteTimeout)
	assert.Equal(t, 9090, cfg.GRPC.Port)
	assert.Equal(t, "localhost", cfg.Consul.Address)
	assert.Equal(t, 8500, cfg.Consul.Port)
	assert.Equal(t, "localhost", cfg.Database.Host)
	assert.Equal(t, 5432, cfg.Database.Port)
	assert.Equal(t, "postgres", cfg.Database.User)
	assert.Equal(t, "streamgate", cfg.Database.Database)
	assert.Equal(t, "disable", cfg.Database.SSLMode)
	assert.Equal(t, 100, cfg.Database.MaxConns)
	assert.Equal(t, 5, cfg.Database.MaxIdleConns)
	assert.Equal(t, "5m", cfg.Database.ConnMaxLifetime)
	assert.Equal(t, "localhost", cfg.Redis.Host)
	assert.Equal(t, 6379, cfg.Redis.Port)
	assert.Equal(t, 100, cfg.Redis.PoolSize)
	assert.Equal(t, "minio", cfg.Storage.Type)
	assert.Equal(t, "localhost:9000", cfg.Storage.Endpoint)
	assert.Equal(t, "streamgate", cfg.Storage.Bucket)
	assert.Equal(t, "us-east-1", cfg.Storage.Region)
	assert.False(t, cfg.Storage.UseSSL)
	assert.Equal(t, "nats://localhost:4222", cfg.NATS.URL)
	assert.Equal(t, int64(11155111), cfg.Web3.ChainID)
	assert.Equal(t, "safe", cfg.Web3.BlockTag)
	assert.True(t, cfg.Web3.Transaction.EIP1559)
	assert.Equal(t, float64(1.2), cfg.Web3.Transaction.GasMultiplier)
	assert.Equal(t, uint64(2), cfg.Web3.Transaction.Confirmations)
	assert.Equal(t, float64(500), cfg.Web3.Transaction.MaxFeePerGasCapGwei)
	assert.True(t, cfg.Transcoding.Enabled)
	assert.Equal(t, 4, cfg.Transcoding.MaxWorkers)
	assert.Equal(t, 100, cfg.Transcoding.QueueSize)
	assert.Equal(t, []string{"hls", "dash"}, cfg.Transcoding.OutputFormats)
	assert.Equal(t, 10, cfg.Streaming.HLSSegmentDuration)
	assert.True(t, cfg.Streaming.CacheEnabled)
	assert.True(t, cfg.RateLimiting.Enabled)
	assert.True(t, cfg.CircuitBreaker.Enabled)
	assert.True(t, cfg.Features.NFTGating)
	assert.True(t, cfg.Features.SignatureAuth)
	assert.True(t, cfg.Features.ChunkedUpload)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

func TestEnvOr(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		_ = os.Setenv("TEST_ENV_OR_KEY", "from-env")
		defer os.Unsetenv("TEST_ENV_OR_KEY")

		result := envOr("TEST_ENV_OR_KEY", "fallback")
		assert.Equal(t, "from-env", result)
	})

	t.Run("returns fallback when env not set", func(t *testing.T) {
		os.Unsetenv("TEST_ENV_OR_MISSING")

		result := envOr("TEST_ENV_OR_MISSING", "fallback")
		assert.Equal(t, "fallback", result)
	})

	t.Run("returns fallback when env is empty", func(t *testing.T) {
		_ = os.Setenv("TEST_ENV_OR_EMPTY", "")
		defer os.Unsetenv("TEST_ENV_OR_EMPTY")

		result := envOr("TEST_ENV_OR_EMPTY", "fallback")
		assert.Equal(t, "fallback", result)
	})
}

func TestExpandEnvWithDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envSetup map[string]string
		expected string
	}{
		{
			name:     "no env vars",
			input:    "plain text",
			envSetup: nil,
			expected: "plain text",
		},
		{
			name:     "simple env var",
			input:    "host: ${TEST_HOST}",
			envSetup: map[string]string{"TEST_HOST": "myhost"},
			expected: "host: myhost",
		},
		{
			name:     "env var with default when not set",
			input:    "host: ${TEST_MISSING_HOST:-localhost}",
			envSetup: nil,
			expected: "host: localhost",
		},
		{
			name:     "env var with default when set",
			input:    "host: ${TEST_HOST_OVERRIDE:-localhost}",
			envSetup: map[string]string{"TEST_HOST_OVERRIDE": "real-host"},
			expected: "host: real-host",
		},
		{
			name:     "multiple env vars",
			input:    "${TEST_A}:${TEST_B}",
			envSetup: map[string]string{"TEST_A": "val-a", "TEST_B": "val-b"},
			expected: "val-a:val-b",
		},
		{
			name:     "empty env var without default",
			input:    "val: ${TEST_EMPTY_EXPAND}",
			envSetup: map[string]string{"TEST_EMPTY_EXPAND": ""},
			expected: "val: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envSetup {
				_ = os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envSetup {
					_ = os.Unsetenv(k)
				}
			}()

			result := expandEnvWithDefaults(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidationError(t *testing.T) {
	t.Run("error with critical and warnings", func(t *testing.T) {
		ve := &ValidationError{
			Critical: []string{"jwt_secret is empty"},
			Warnings: []string{"minioadmin detected"},
		}

		msg := ve.Error()
		assert.Contains(t, msg, "CRITICAL: jwt_secret is empty")
		assert.Contains(t, msg, "WARNING:  minioadmin detected")
		assert.Contains(t, msg, "production config validation failed")
	})

	t.Run("HasCritical returns true", func(t *testing.T) {
		ve := &ValidationError{Critical: []string{"err"}}
		assert.True(t, ve.HasCritical())
	})

	t.Run("HasCritical returns false", func(t *testing.T) {
		ve := &ValidationError{Warnings: []string{"warn"}}
		assert.False(t, ve.HasCritical())
	})

	t.Run("error with fix hints for JWT", func(t *testing.T) {
		ve := &ValidationError{Critical: []string{"auth.jwt_secret is empty"}}
		msg := ve.Error()
		assert.Contains(t, msg, "Set STREAMGATE_JWT_SECRET")
	})

	t.Run("error with fix hints for DB password", func(t *testing.T) {
		ve := &ValidationError{Critical: []string{"database.password uses insecure default"}}
		msg := ve.Error()
		assert.Contains(t, msg, "Set STREAMGATE_DB_PASSWORD")
	})

	t.Run("error with fix hints for storage", func(t *testing.T) {
		ve := &ValidationError{Warnings: []string{"minioadmin detected", "use_ssl is disabled"}}
		msg := ve.Error()
		assert.Contains(t, msg, "Set STREAMGATE_STORAGE_ACCESS_KEY")
		assert.Contains(t, msg, "Set STREAMGATE_STORAGE_USE_SSL=true")
	})

	t.Run("error with fix hints for YOUR_KEY", func(t *testing.T) {
		ve := &ValidationError{Warnings: []string{"contains placeholder YOUR_KEY"}}
		msg := ve.Error()
		assert.Contains(t, msg, "Set WEB3_ETHEREUM_RPC")
	})
}

func TestValidateProduction(t *testing.T) {
	t.Run("monolith mode insecure", func(t *testing.T) {
		cfg := &Config{
			Mode:     "monolith",
			Auth:     AuthConfig{JWTSecret: "streamgate-dev-secret"},
			Storage:  StorageConfig{AccessKey: "minioadmin", SecretKey: "minioadmin"},
			Database: DatabaseConfig{SSLMode: "disable"},
			Web3:     Web3Config{EthereumRPC: "https://sepolia.infura.io/v3/YOUR_KEY"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("microservice mode insecure", func(t *testing.T) {
		cfg := &Config{
			Mode:     "microservice",
			Auth:     AuthConfig{JWTSecret: "streamgate-dev-secret"},
			Storage:  StorageConfig{AccessKey: "minioadmin", SecretKey: "minioadmin"},
			Database: DatabaseConfig{SSLMode: "disable"},
			Web3:     Web3Config{EthereumRPC: "https://sepolia.infura.io/v3/YOUR_KEY"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("insecure JWT secret", func(t *testing.T) {
		cfg := &Config{
			Mode:     "monolith",
			Auth:     AuthConfig{JWTSecret: "streamgate-dev-secret"},
			Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret"},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("passes with secure config", func(t *testing.T) {
		cfg := &Config{
			Mode:     "microservice",
			Auth:     AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			Redis:    RedisConfig{Password: "redis-pw"},
			NATS:     NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.NoError(t, err)
	})

	t.Run("YAML default JWT secret", func(t *testing.T) {
		cfg := &Config{
			Mode:     "microservice",
			Auth:     AuthConfig{JWTSecret: "your-secret-key-change-in-production"},
			Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret"},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("insecure DB passwords", func(t *testing.T) {
		tests := []struct {
			name     string
			password string
			wantErr  bool
		}{
			{"streamgate_password", "streamgate_password", true},
			{"streamgate_dev_password", "streamgate_dev_password", true},
			{"secure password", "a-secure-production-pw", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := &Config{
					Mode:     "microservice",
					Auth:     AuthConfig{JWTSecret: "a-real-secret-that-is-at-least-32-chars"},
					Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
					Database: DatabaseConfig{SSLMode: "require", Password: tt.password},
					Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
					Redis:    RedisConfig{Password: "redis-pw"},
					NATS:     NATSConfig{URL: "nats://localhost:4222"},
				}
				err := cfg.ValidateProduction(zap.NewNop())
				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("empty JWT secret", func(t *testing.T) {
		cfg := &Config{
			Mode:     "microservice",
			Auth:     AuthConfig{JWTSecret: ""},
			Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret"},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("JWT secret too short", func(t *testing.T) {
		cfg := &Config{
			Mode:     "microservice",
			Auth:     AuthConfig{JWTSecret: "short"},
			Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			Redis:    RedisConfig{Password: "redis-pw"},
			NATS:     NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("insecure JWT secrets table", func(t *testing.T) {
		insecureSecrets := []string{
			"streamgate-dev-secret-32chars!!",
			"streamgate-dev-secret",
			"your-secret-key-change-in-production",
			"dev-secret-key-not-for-production",
		}
		for _, secret := range insecureSecrets {
			t.Run(secret, func(t *testing.T) {
				cfg := &Config{
					Auth:    AuthConfig{JWTSecret: secret},
					Storage: StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
					Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
					Web3:    Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
					Redis:   RedisConfig{Password: "redis-pw"},
					NATS:    NATSConfig{URL: "nats://localhost:4222"},
				}
				err := cfg.ValidateProduction(zap.NewNop())
				assert.Error(t, err)
			})
		}
	})

	t.Run("CORS wildcard", func(t *testing.T) {
		cfg := &Config{
			Auth:     AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret"},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			CORS:     CORSConfig{AllowedOrigins: []string{"*"}},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("CORS restrictive", func(t *testing.T) {
		cfg := &Config{
			Auth:     AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage:  StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:     Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			CORS:     CORSConfig{AllowedOrigins: []string{"https://streamgate.example.com"}},
			Redis:    RedisConfig{Password: "redis-pw"},
			NATS:     NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.NoError(t, err)
	})

	t.Run("private key hex invalid length", func(t *testing.T) {
		cfg := &Config{
			Auth:    AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage: StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3: Web3Config{
				EthereumRPC: "https://mainnet.infura.io/v3/real-key",
				Transaction: TransactionConfig{
					PrivateKeyHex: "0x1234",
				},
			},
			Redis: RedisConfig{Password: "redis-pw"},
			NATS:  NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("private key hex invalid encoding", func(t *testing.T) {
		cfg := &Config{
			Auth:    AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage: StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3: Web3Config{
				EthereumRPC: "https://mainnet.infura.io/v3/real-key",
				Transaction: TransactionConfig{
					PrivateKeyHex: hex.EncodeToString([]byte("not-64-chars-but-also-not-valid-hex-zzzzzzzzzzz")),
				},
			},
			Redis: RedisConfig{Password: "redis-pw"},
			NATS:  NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("empty redis password warning", func(t *testing.T) {
		cfg := &Config{
			Auth:    AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage: StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:    Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			Redis:   RedisConfig{Password: ""},
			NATS:    NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("empty NATS URL warning", func(t *testing.T) {
		cfg := &Config{
			Auth:    AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage: StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:    Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			Redis:   RedisConfig{Password: "redis-pw"},
			NATS:    NATSConfig{URL: ""},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("s3 storage with empty endpoint warning", func(t *testing.T) {
		cfg := &Config{
			Auth:    AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage: StorageConfig{Type: "s3", AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true, Endpoint: ""},
			Database: DatabaseConfig{SSLMode: "require", Password: "secure-pw"},
			Web3:    Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			Redis:   RedisConfig{Password: "redis-pw"},
			NATS:    NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})

	t.Run("database sslmode disable warning", func(t *testing.T) {
		cfg := &Config{
			Auth:    AuthConfig{JWTSecret: "a-real-production-secret-32chars!!"},
			Storage: StorageConfig{AccessKey: "real-key", SecretKey: "real-secret", UseSSL: true},
			Database: DatabaseConfig{SSLMode: "disable", Password: "secure-pw"},
			Web3:    Web3Config{EthereumRPC: "https://mainnet.infura.io/v3/real-key"},
			Redis:   RedisConfig{Password: "redis-pw"},
			NATS:    NATSConfig{URL: "nats://localhost:4222"},
		}
		err := cfg.ValidateProduction(zap.NewNop())
		assert.Error(t, err)
	})
}

func TestConfigManager(t *testing.T) {
	t.Run("new config manager", func(t *testing.T) {
		cm := NewConfigManager("/tmp/test-config.yaml", zap.NewNop())
		assert.NotNil(t, cm)
		assert.Nil(t, cm.Get())
		assert.False(t, cm.IsHotReloadEnabled())
	})

	t.Run("load from yaml file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yamlContent := `appname: test-app
mode: monolith
server:
  port: 9090
  readtimeout: 10
  writetimeout: 10
database:
  host: localhost
  port: 5432
  user: test
  password: test
  database: testdb
  sslmode: disable
redis:
  host: localhost
  port: 6379
storage:
  type: minio
  endpoint: localhost:9000
grpc:
  port: 9091
`
		require.NoError(t, os.WriteFile(path, []byte(yamlContent), 0o644))

		cm := NewConfigManager(path, zap.NewNop())
		err := cm.Load()
		require.NoError(t, err)

		cfg := cm.Get()
		require.NotNil(t, cfg)
		assert.Equal(t, "test-app", cfg.AppName)
		assert.Equal(t, "monolith", cfg.Mode)
	})

	t.Run("save and reload", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.config = DefaultConfig()

		err := cm.Save()
		require.NoError(t, err)

		cm2 := NewConfigManager(path, zap.NewNop())
		err = cm2.Load()
		require.NoError(t, err)

		cfg := cm2.Get()
		assert.NotNil(t, cfg)
		assert.Equal(t, "streamgate", cfg.AppName)
	})

	t.Run("save without config returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		err := cm.Save()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no configuration to save")
	})

	t.Run("validate loaded config", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		err := cm.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not loaded")

		cm.config = &Config{
			Server:   ServerConfig{Port: 8080},
			Database: DatabaseConfig{Host: "localhost"},
		}
		err = cm.Validate()
		assert.NoError(t, err)
	})

	t.Run("validate invalid port", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.config = &Config{
			Server:   ServerConfig{Port: 0},
			Database: DatabaseConfig{Host: "localhost"},
		}
		err := cm.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid server port")
	})

	t.Run("validate missing db host", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.config = &Config{
			Server:   ServerConfig{Port: 8080},
			Database: DatabaseConfig{Host: ""},
		}
		err := cm.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database host is required")
	})

	t.Run("update config", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.config = DefaultConfig()

		newCfg := DefaultConfig()
		newCfg.AppName = "updated-app"
		err := cm.Update(newCfg)
		require.NoError(t, err)

		assert.Equal(t, "updated-app", cm.Get().AppName)
	})

	t.Run("update with change handler", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.config = DefaultConfig()

		var handlerCalled bool
		cm.AddChangeHandler(func(old, new_ *Config) error {
			handlerCalled = true
			assert.Equal(t, "streamgate", old.AppName)
			assert.Equal(t, "new-app", new_.AppName)
			return nil
		})

		newCfg := DefaultConfig()
		newCfg.AppName = "new-app"
		require.NoError(t, cm.Update(newCfg))
		assert.True(t, handlerCalled)
	})

	t.Run("update with failing handler", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.config = DefaultConfig()

		cm.AddChangeHandler(func(old, new_ *Config) error {
			return assert.AnError
		})

		newCfg := DefaultConfig()
		newCfg.AppName = "new-app"
		err := cm.Update(newCfg)
		assert.NoError(t, err)
	})

	t.Run("add and remove change handler", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())

		idx := cm.AddChangeHandler(func(old, new_ *Config) error { return nil })
		assert.Equal(t, 0, idx)

		idx2 := cm.AddChangeHandler(func(old, new_ *Config) error { return nil })
		assert.Equal(t, 1, idx2)

		cm.RemoveChangeHandler(0)
	})

	t.Run("remove invalid handler index", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.RemoveChangeHandler(-1)
		cm.RemoveChangeHandler(99)
	})

	t.Run("hot reload toggle", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		assert.False(t, cm.IsHotReloadEnabled())

		cm.SetHotReload(true)
		assert.True(t, cm.IsHotReloadEnabled())

		cm.SetHotReload(false)
		assert.False(t, cm.IsHotReloadEnabled())
	})

	t.Run("load with change handler", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		cm := NewConfigManager(path, zap.NewNop())
		cm.config = DefaultConfig()

		var handlerCalled bool
		cm.AddChangeHandler(func(old, new_ *Config) error {
			handlerCalled = true
			return nil
		})

		yamlContent := `appname: reloaded
mode: monolith
server:
  port: 8080
database:
  host: localhost
  port: 5432
storage:
  endpoint: localhost:9000
grpc:
  port: 9090
`
		require.NoError(t, os.WriteFile(path, []byte(yamlContent), 0o644))

		require.NoError(t, cm.Load())
		assert.True(t, handlerCalled)
	})
}

func TestLoadOrCreate(t *testing.T) {
	t.Run("creates default when file not found", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "missing-config.yaml")

		cm, err := LoadOrCreate(path, zap.NewNop())
		require.NoError(t, err)
		require.NotNil(t, cm)
		cfg := cm.Get()
		assert.NotNil(t, cfg)
		assert.Equal(t, "streamgate", cfg.AppName)
	})
}

func TestChainConfigEntry(t *testing.T) {
	entry := ChainConfigEntry{
		ID:          1,
		Name:        "Ethereum",
		RPC:         "https://mainnet.infura.io/v3/key",
		RPCs:        []string{"https://rpc1.eth.com", "https://rpc2.eth.com"},
		ExplorerURL: "https://etherscan.io",
		Currency:    "ETH",
		IsTestnet:   false,
	}

	assert.Equal(t, int64(1), entry.ID)
	assert.Equal(t, "Ethereum", entry.Name)
	assert.Equal(t, 2, len(entry.RPCs))
	assert.False(t, entry.IsTestnet)
}

func TestRPCRateLimitConfig(t *testing.T) {
	cfg := RPCRateLimitConfig{
		Enabled: true,
		Rate:    10.5,
		Burst:   20.0,
	}

	assert.True(t, cfg.Enabled)
	assert.Equal(t, float64(10.5), cfg.Rate)
	assert.Equal(t, float64(20.0), cfg.Burst)
}

func TestTransactionConfig(t *testing.T) {
	cfg := TransactionConfig{
		GasLimit:                 21000,
		GasMultiplier:            1.2,
		Confirmations:            3,
		MaxFeePerGasGwei:         100,
		MaxFeePerGasCapGwei:      500,
		MaxPriorityFeePerGasGwei: 2,
		EIP1559:                  true,
	}

	assert.Equal(t, uint64(21000), cfg.GasLimit)
	assert.Equal(t, float64(1.2), cfg.GasMultiplier)
	assert.True(t, cfg.EIP1559)
}
