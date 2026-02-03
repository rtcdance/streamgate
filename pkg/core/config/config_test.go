package config

import (
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetDefaults(t *testing.T) {
	t.Run("set default values", func(t *testing.T) {
		setDefaults()

		assert.Equal(t, "streamgate", viper.GetString("app.name"))
		assert.Equal(t, "monolith", viper.GetString("app.mode"))
		assert.Equal(t, 8080, viper.GetInt("app.port"))
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
		assert.Equal(t, "http://localhost:14268/api/traces", viper.GetString("monitoring.jaeger_endpoint"))
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
				Password: "secret",
				Database: "production",
				SSLMode:  "require",
			},
			expected: "postgres://admin:secret@db.example.com:5433/production?sslmode=require",
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
		assert.Equal(t, 8080, cfg.Port)
		assert.Equal(t, false, cfg.Debug)
	})

	t.Run("load config with environment variables", func(t *testing.T) {
		os.Setenv("DATABASE_HOST", "custom-host")
		os.Setenv("DATABASE_PORT", "5433")
		os.Setenv("DATABASE_USER", "custom-user")
		os.Setenv("DATABASE_PASSWORD", "custom-pass")
		os.Setenv("DATABASE_NAME", "custom-db")
		os.Setenv("DATABASE_SSLMODE", "require")
		os.Setenv("DATABASE_MAXCONNS", "50")

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

		os.Unsetenv("DATABASE_HOST")
		os.Unsetenv("DATABASE_PORT")
		os.Unsetenv("DATABASE_USER")
		os.Unsetenv("DATABASE_PASSWORD")
		os.Unsetenv("DATABASE_NAME")
		os.Unsetenv("DATABASE_SSLMODE")
		os.Unsetenv("DATABASE_MAXCONNS")
	})
}
