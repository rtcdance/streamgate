package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.yaml.in/yaml/v3"
)

// Config holds the application configuration
type Config struct {
	// Application
	AppName     string
	Mode        string // "monolith" or "microservice"
	ServiceName string // for microservice mode
	Port        int
	Debug       bool

	// Server
	Server ServerConfig

	// gRPC (for microservice mode)
	GRPC GRPCConfig

	// Database
	Database DatabaseConfig

	// Redis
	Redis RedisConfig

	// Storage
	Storage StorageConfig

	// NATS (for microservices mode)
	NATS NATSConfig

	// Consul (for service discovery)
	Consul ConsulConfig

	// Transcoding
	Transcoding TranscodingConfig

	// Streaming
	Streaming StreamingConfig

	// Web3
	Web3 Web3Config

	// Auth
	Auth AuthConfig

	// Rate Limiting
	RateLimiting RateLimitingConfig

	// Circuit Breaker
	CircuitBreaker CircuitBreakerConfig

	// Monitoring
	Monitoring MonitoringConfig

	// Logging
	Logging LoggingConfig

	// CORS
	CORS CORSConfig

	// Features
	Features FeaturesConfig

	// Upload
	Upload UploadConfig

	// Transcode
	Transcode TranscodeConfig

	// Plugins (for monolithic mode)
	Plugins PluginsConfig
}

type UploadConfig struct {
	MaxSize        int64    `yaml:"max_size"`
	StorageQuota   int64    `yaml:"storage_quota"`
	AllowedFormats []string `yaml:"allowed_formats"`
	MaxChunks      int      `yaml:"max_chunks"`
}

type TranscodeConfig struct {
	Profiles []string `yaml:"profiles"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port         int
	ReadTimeout  int
	WriteTimeout int
}

// GRPCConfig holds gRPC configuration
type GRPCConfig struct {
	Port       int
	TLSEnabled bool   `yaml:"tls_enabled"`
	TLSCert    string `yaml:"tls_cert"`
	TLSKey     string `yaml:"tls_key"`
}

// ConsulConfig holds Consul configuration
type ConsulConfig struct {
	Address string
	Port    int
}

// PluginsConfig holds plugin configuration
type PluginsConfig struct {
	Enabled []string
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	SSLMode         string
	MaxConns        int
	MaxIdleConns    int
	ConnMaxLifetime string
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	Type      string // "s3" or "minio"
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
}

// TranscodingConfig holds transcoding configuration
type TranscodingConfig struct {
	Enabled       bool
	MaxWorkers    int
	QueueSize     int
	OutputFormats []string
}

// StreamingConfig holds streaming configuration
type StreamingConfig struct {
	HLSSegmentDuration   int
	DASHSegmentDuration  int
	CacheEnabled         bool
	CacheTTL             string
	MaxConcurrentStreams int
}

// RateLimitingConfig holds rate limiting configuration
type RateLimitingConfig struct {
	Enabled           bool
	RequestsPerMinute int
	RequestsPerHour   int
	BurstSize         int
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level      string
	Format     string
	Output     string
	File       string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// FeaturesConfig holds feature flags
type FeaturesConfig struct {
	NFTGating       bool
	SignatureAuth   bool
	ChunkedUpload   bool
	ResumableUpload bool
	AdaptiveBitrate bool
	MultiCodec      bool
}

// NATSConfig holds NATS configuration
type NATSConfig struct {
	URL string
}

// Web3Config holds Web3 configuration
type Web3Config struct {
	EthereumRPC   string
	EthereumWSURL string // WebSocket URL for real-time event subscriptions
	SolanaRPC     string
	ChainID       int64
	BlockTag      string // "safe" (default), "finalized", or "latest"
	Transaction   TransactionConfig
	RateLimit     RPCRateLimitConfig
}

// RPCRateLimitConfig holds RPC rate limiting configuration
type RPCRateLimitConfig struct {
	Enabled bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	Rate    float64 `mapstructure:"rate" json:"rate" yaml:"rate"`    // requests per second per RPC endpoint
	Burst   float64 `mapstructure:"burst" json:"burst" yaml:"burst"` // max burst size
}

// TransactionConfig holds on-chain transaction parameters
type TransactionConfig struct {
	PrivateKeyHex            string  // hex-encoded private key for signing
	GasLimit                 uint64  // default gas limit
	GasMultiplier            float64 // multiplier on estimated gas (e.g. 1.2 for 20% buffer)
	Confirmations            uint64  // number of block confirmations to wait
	MaxFeePerGasGwei         float64 // max fee per gas in Gwei (EIP-1559 floor)
	MaxFeePerGasCapGwei      float64 // hard cap on max fee per gas in Gwei (safety limit, default 500)
	MaxPriorityFeePerGasGwei float64 // max priority fee per gas in Gwei (EIP-1559 tip)
	EIP1559                  bool    // use EIP-1559 dynamic fee transactions when true
}

// MonitoringConfig holds monitoring configuration
type MonitoringConfig struct {
	PrometheusPort int
	JaegerEndpoint string
	LogLevel       string
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWTSecret          string
	JWTExpiry          string
	RefreshTokenExpiry string
	NonceExpiry        string
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins []string
}

// CircuitBreakerConfig holds circuit breaker configuration
type CircuitBreakerConfig struct {
	Enabled          bool
	FailureThreshold int
	SuccessThreshold int
	Timeout          string
	MaxRequests      int
	WindowTime       string
}

// LoadConfig loads configuration from environment and config files.
// Reads base config.yaml first, then merges environment-specific config
// (config.{STREAMGATE_ENV}.yaml). STREAMGATE_ENV defaults to "dev".
func LoadConfig() (*Config, error) {
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")

	// Set defaults
	setDefaults()

	// Read from environment - allow DATABASE_ prefix to map to database.* keys
	viper.SetEnvPrefix("")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Explicitly bind environment variables for database config
	_ = viper.BindEnv("database.host", "DATABASE_HOST")
	_ = viper.BindEnv("database.port", "DATABASE_PORT")
	_ = viper.BindEnv("database.user", "DATABASE_USER")
	_ = viper.BindEnv("database.password", "DATABASE_PASSWORD")
	_ = viper.BindEnv("database.database", "DATABASE_NAME")
	_ = viper.BindEnv("database.sslmode", "DATABASE_SSLMODE")
	_ = viper.BindEnv("database.maxconns", "DATABASE_MAXCONNS")

	// Explicitly bind environment variables for Redis config
	_ = viper.BindEnv("redis.host", "REDIS_HOST")
	_ = viper.BindEnv("redis.port", "REDIS_PORT")
	_ = viper.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = viper.BindEnv("redis.db", "REDIS_DB")

	// Explicitly bind environment variables for Storage config
	_ = viper.BindEnv("storage.type", "STORAGE_TYPE")
	_ = viper.BindEnv("storage.endpoint", "STORAGE_ENDPOINT")
	_ = viper.BindEnv("storage.accesskey", "STORAGE_ACCESSKEY")
	_ = viper.BindEnv("storage.secretkey", "STORAGE_SECRETKEY")
	_ = viper.BindEnv("storage.bucket", "STORAGE_BUCKET")
	_ = viper.BindEnv("storage.region", "STORAGE_REGION")
	_ = viper.BindEnv("storage.use_ssl", "STORAGE_USE_SSL")

	_ = viper.BindEnv("database.max_idle_conns", "DATABASE_MAX_IDLE_CONNS")
	_ = viper.BindEnv("database.conn_max_lifetime", "DATABASE_CONN_MAX_LIFETIME")

	// Explicitly bind environment variables for NATS config
	_ = viper.BindEnv("nats.url", "NATS_URL")

	// Explicitly bind environment variables for Consul config
	_ = viper.BindEnv("consul.address", "CONSUL_HOST")
	_ = viper.BindEnv("consul.port", "CONSUL_PORT")

	// Explicitly bind environment variables for monitoring
	_ = viper.BindEnv("monitoring.jaeger_endpoint", "JAEGER_ENDPOINT")

	// Explicitly bind environment variables for Web3
	_ = viper.BindEnv("web3.ethereum_rpc", "WEB3_ETHEREUM_RPC")
	_ = viper.BindEnv("web3.solana_rpc", "WEB3_SOLANA_RPC")
	_ = viper.BindEnv("auth.jwt_secret", "AUTH_JWT_SECRET")
	_ = viper.BindEnv("cors.allowed_origins", "CORS_ALLOWED_ORIGINS")

	// Read base config file
	viper.SetConfigName("config")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found; using environment variables and defaults
	}

	// Merge environment-specific config (overrides base values)
	env := os.Getenv("STREAMGATE_ENV")
	if env == "" {
		env = "dev"
	}
	viper.SetConfigName("config." + env)
	if err := viper.MergeInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok { //nolint:staticcheck
		}
	}

	cfg := &Config{
		AppName:     viper.GetString("app.name"),
		Mode:        viper.GetString("app.mode"),
		ServiceName: viper.GetString("app.service_name"),
		Port:        viper.GetInt("app.port"),
		Debug:       viper.GetBool("app.debug"),

		Server: ServerConfig{
			Port:         viper.GetInt("server.port"),
			ReadTimeout:  viper.GetInt("server.read_timeout"),
			WriteTimeout: viper.GetInt("server.write_timeout"),
		},

		GRPC: GRPCConfig{
			Port: viper.GetInt("grpc.port"),
		},

		Consul: ConsulConfig{
			Address: viper.GetString("consul.address"),
			Port:    viper.GetInt("consul.port"),
		},

		Database: DatabaseConfig{
			Host:            viper.GetString("database.host"),
			Port:            viper.GetInt("database.port"),
			User:            viper.GetString("database.user"),
			Password:        viper.GetString("database.password"),
			Database:        viper.GetString("database.database"),
			SSLMode:         viper.GetString("database.sslmode"),
			MaxConns:        viper.GetInt("database.maxconns"),
			MaxIdleConns:    viper.GetInt("database.max_idle_conns"),
			ConnMaxLifetime: viper.GetString("database.conn_max_lifetime"),
		},

		Redis: RedisConfig{
			Host:     viper.GetString("redis.host"),
			Port:     viper.GetInt("redis.port"),
			Password: viper.GetString("redis.password"),
			DB:       viper.GetInt("redis.db"),
			PoolSize: viper.GetInt("redis.poolsize"),
		},

		Storage: StorageConfig{
			Type:      viper.GetString("storage.type"),
			Endpoint:  viper.GetString("storage.endpoint"),
			AccessKey: viper.GetString("storage.accesskey"),
			SecretKey: viper.GetString("storage.secretkey"),
			Bucket:    viper.GetString("storage.bucket"),
			Region:    viper.GetString("storage.region"),
			UseSSL:    viper.GetBool("storage.use_ssl"),
		},

		NATS: NATSConfig{
			URL: viper.GetString("nats.url"),
		},

		Web3: Web3Config{
			EthereumRPC:   viper.GetString("web3.ethereum_rpc"),
			EthereumWSURL: viper.GetString("web3.ethereum_ws_url"),
			SolanaRPC:     viper.GetString("web3.solana_rpc"),
			ChainID:       viper.GetInt64("web3.chain_id"),
			BlockTag:      viper.GetString("web3.block_tag"),
			Transaction: TransactionConfig{
				PrivateKeyHex:            viper.GetString("web3.transaction.private_key_hex"),
				GasLimit:                 viper.GetUint64("web3.transaction.gas_limit"),
				GasMultiplier:            viper.GetFloat64("web3.transaction.gas_multiplier"),
				Confirmations:            viper.GetUint64("web3.transaction.confirmations"),
				MaxFeePerGasGwei:         viper.GetFloat64("web3.transaction.max_fee_per_gas_gwei"),
				MaxFeePerGasCapGwei:      viper.GetFloat64("web3.transaction.max_fee_per_gas_cap_gwei"),
				MaxPriorityFeePerGasGwei: viper.GetFloat64("web3.transaction.max_priority_fee_per_gas_gwei"),
				EIP1559:                  viper.GetBool("web3.transaction.eip1559"),
			},
			RateLimit: RPCRateLimitConfig{
				Enabled: viper.GetBool("web3.rate_limit.enabled"),
				Rate:    viper.GetFloat64("web3.rate_limit.rate"),
				Burst:   viper.GetFloat64("web3.rate_limit.burst"),
			},
		},

		Monitoring: MonitoringConfig{
			PrometheusPort: viper.GetInt("monitoring.prometheus_port"),
			JaegerEndpoint: viper.GetString("monitoring.jaeger_endpoint"),
			LogLevel:       viper.GetString("monitoring.log_level"),
		},

		Transcoding: TranscodingConfig{
			Enabled:       viper.GetBool("transcoding.enabled"),
			MaxWorkers:    viper.GetInt("transcoding.max_workers"),
			QueueSize:     viper.GetInt("transcoding.queue_size"),
			OutputFormats: viper.GetStringSlice("transcoding.output_formats"),
		},

		Streaming: StreamingConfig{
			HLSSegmentDuration:   viper.GetInt("streaming.hls_segment_duration"),
			DASHSegmentDuration:  viper.GetInt("streaming.dash_segment_duration"),
			CacheEnabled:         viper.GetBool("streaming.cache_enabled"),
			CacheTTL:             viper.GetString("streaming.cache_ttl"),
			MaxConcurrentStreams: viper.GetInt("streaming.max_concurrent_streams"),
		},

		RateLimiting: RateLimitingConfig{
			Enabled:           viper.GetBool("rate_limiting.enabled"),
			RequestsPerMinute: viper.GetInt("rate_limiting.requests_per_minute"),
			RequestsPerHour:   viper.GetInt("rate_limiting.requests_per_hour"),
			BurstSize:         viper.GetInt("rate_limiting.burst_size"),
		},

		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          viper.GetBool("circuit_breaker.enabled"),
			FailureThreshold: viper.GetInt("circuit_breaker.failure_threshold"),
			SuccessThreshold: viper.GetInt("circuit_breaker.success_threshold"),
			Timeout:          viper.GetString("circuit_breaker.timeout"),
			MaxRequests:      viper.GetInt("circuit_breaker.max_requests"),
			WindowTime:       viper.GetString("circuit_breaker.window_time"),
		},

		Logging: LoggingConfig{
			Level:      viper.GetString("logging.level"),
			Format:     viper.GetString("logging.format"),
			Output:     viper.GetString("logging.output"),
			File:       viper.GetString("logging.file"),
			MaxSize:    viper.GetInt("logging.max_size"),
			MaxBackups: viper.GetInt("logging.max_backups"),
			MaxAge:     viper.GetInt("logging.max_age"),
			Compress:   viper.GetBool("logging.compress"),
		},

		Features: FeaturesConfig{
			NFTGating:       viper.GetBool("features.nft_gating"),
			SignatureAuth:   viper.GetBool("features.signature_auth"),
			ChunkedUpload:   viper.GetBool("features.chunked_upload"),
			ResumableUpload: viper.GetBool("features.resumable_upload"),
			AdaptiveBitrate: viper.GetBool("features.adaptive_bitrate"),
			MultiCodec:      viper.GetBool("features.multi_codec"),
		},

		Auth: AuthConfig{
			JWTSecret:          viper.GetString("auth.jwt_secret"),
			JWTExpiry:          viper.GetString("auth.jwt_expiry"),
			RefreshTokenExpiry: viper.GetString("auth.refresh_token_expiry"),
			NonceExpiry:        viper.GetString("auth.nonce_expiry"),
		},

		CORS: CORSConfig{
			AllowedOrigins: viper.GetStringSlice("cors.allowed_origins"),
		},

		Plugins: PluginsConfig{
			Enabled: viper.GetStringSlice("plugins.enabled"),
		},
	}

	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return nil, fmt.Errorf("invalid server port: %d", cfg.Server.Port)
	}

	if cfg.Database.Host == "" {
		return nil, fmt.Errorf("database host is required")
	}

	return cfg, nil
}

// setDefaults sets default configuration values
func setDefaults() {
	// Application defaults
	viper.SetDefault("app.name", "streamgate")
	viper.SetDefault("app.mode", "monolith")
	viper.SetDefault("app.service_name", "")
	viper.SetDefault("app.port", 8080)
	viper.SetDefault("app.debug", false)

	// Server defaults
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.read_timeout", 30)
	viper.SetDefault("server.write_timeout", 30)

	// gRPC defaults
	viper.SetDefault("grpc.port", 9090)

	// Consul defaults
	viper.SetDefault("consul.address", "localhost")
	viper.SetDefault("consul.port", 8500)

	// Database defaults
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.user", "postgres")
	viper.SetDefault("database.password", "postgres")
	viper.SetDefault("database.database", "streamgate")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("database.maxconns", 100)
	viper.SetDefault("database.max_idle_conns", 5)
	viper.SetDefault("database.conn_max_lifetime", "5m")

	// Redis defaults
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.poolsize", 100)

	// Storage defaults
	viper.SetDefault("storage.type", "minio")
	viper.SetDefault("storage.endpoint", "localhost:9000")
	viper.SetDefault("storage.accesskey", "minioadmin")
	viper.SetDefault("storage.secretkey", "minioadmin")
	viper.SetDefault("storage.bucket", "streamgate")
	viper.SetDefault("storage.region", "us-east-1")
	viper.SetDefault("storage.use_ssl", false)

	// NATS defaults
	viper.SetDefault("nats.url", "nats://localhost:4222")

	// Web3 defaults
	viper.SetDefault("web3.ethereum_rpc", "https://sepolia.infura.io/v3/YOUR_KEY")
	viper.SetDefault("web3.solana_rpc", "https://api.devnet.solana.com")
	viper.SetDefault("web3.chain_id", 11155111) // Sepolia
	viper.SetDefault("web3.block_tag", "safe")

	// Monitoring defaults
	viper.SetDefault("monitoring.prometheus_port", 9090)
	viper.SetDefault("monitoring.jaeger_endpoint", "http://localhost:14268/api/traces")
	viper.SetDefault("monitoring.log_level", "info")

	// Transcoding defaults
	viper.SetDefault("transcoding.enabled", true)
	viper.SetDefault("transcoding.max_workers", 4)
	viper.SetDefault("transcoding.queue_size", 100)
	viper.SetDefault("transcoding.output_formats", []string{"hls", "dash"})

	// Streaming defaults
	viper.SetDefault("streaming.hls_segment_duration", 10)
	viper.SetDefault("streaming.dash_segment_duration", 10)
	viper.SetDefault("streaming.cache_enabled", true)
	viper.SetDefault("streaming.cache_ttl", "3600s")
	viper.SetDefault("streaming.max_concurrent_streams", 1000)

	// Rate limiting defaults
	viper.SetDefault("rate_limiting.enabled", true)
	viper.SetDefault("rate_limiting.requests_per_minute", 60)
	viper.SetDefault("rate_limiting.requests_per_hour", 1000)
	viper.SetDefault("rate_limiting.burst_size", 10)

	// Circuit breaker defaults
	viper.SetDefault("circuit_breaker.enabled", true)
	viper.SetDefault("circuit_breaker.failure_threshold", 5)
	viper.SetDefault("circuit_breaker.success_threshold", 2)
	viper.SetDefault("circuit_breaker.timeout", "30s")
	viper.SetDefault("circuit_breaker.max_requests", 3)
	viper.SetDefault("circuit_breaker.window_time", "1m")

	// Logging defaults
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")
	viper.SetDefault("logging.output", "stdout")
	viper.SetDefault("logging.file", "")
	viper.SetDefault("logging.max_size", 100)
	viper.SetDefault("logging.max_backups", 3)
	viper.SetDefault("logging.max_age", 28)
	viper.SetDefault("logging.compress", true)

	// Features defaults
	viper.SetDefault("features.nft_gating", true)
	viper.SetDefault("features.signature_auth", true)
	viper.SetDefault("features.chunked_upload", true)
	viper.SetDefault("features.resumable_upload", true)

	// Upload defaults
	viper.SetDefault("upload.max_size", 500*1024*1024)
	viper.SetDefault("upload.storage_quota", 50*1024*1024*1024)
	viper.SetDefault("upload.allowed_formats", []string{".mp4", ".webm", ".avi", ".mkv", ".mov", ".mpeg", ".mpg"})
	viper.SetDefault("upload.max_chunks", 10000)
	viper.SetDefault("transcode.profiles", []string{"720p"})
	viper.SetDefault("features.adaptive_bitrate", true)
	viper.SetDefault("features.multi_codec", true)

	// Auth defaults: must set via AUTH_JWT_SECRET env var
	viper.SetDefault("auth.jwt_secret", "")
	viper.SetDefault("auth.nonce_expiry", "5m")

	// CORS defaults
	viper.SetDefault("cors.allowed_origins", []string{})

	// Plugins defaults
	viper.SetDefault("plugins.enabled", []string{})
}

// GetDSN returns the database connection string
func (c *DatabaseConfig) GetDSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User,
		c.Password,
		c.Host,
		c.Port,
		c.Database,
		c.SSLMode,
	)
}

type ValidationError struct {
	Critical []string
	Warnings []string
}

func (e *ValidationError) Error() string {
	var all []string
	all = append(all, e.Critical...)
	all = append(all, e.Warnings...)
	return fmt.Sprintf("production config validation failed — insecure defaults: %s", strings.Join(all, "; "))
}

func (e *ValidationError) HasCritical() bool {
	return len(e.Critical) > 0
}

func (c *Config) ValidateProduction(log *zap.Logger) error {
	var ve ValidationError

	if c.Auth.JWTSecret == "" {
		ve.Critical = append(ve.Critical, "auth.jwt_secret is empty — set via AUTH_JWT_SECRET env var")
	}
	if len(c.Auth.JWTSecret) > 0 && len(c.Auth.JWTSecret) < 32 {
		ve.Critical = append(ve.Critical, fmt.Sprintf("auth.jwt_secret is only %d bytes — minimum 32 bytes required for HS256", len(c.Auth.JWTSecret)))
	}
	insecureSecrets := []string{
		"streamgate-dev-secret-32chars!!",
		"streamgate-dev-secret",
		"your-secret-key-change-in-production",
		"dev-secret-key-not-for-production",
	}
	for _, s := range insecureSecrets {
		if c.Auth.JWTSecret == s {
			ve.Critical = append(ve.Critical, fmt.Sprintf("auth.jwt_secret uses insecure default '%s'", s))
			break
		}
	}

	if c.Storage.AccessKey == "minioadmin" || c.Storage.SecretKey == "minioadmin" {
		ve.Warnings = append(ve.Warnings, "storage credentials use dev default 'minioadmin'")
	}
	if !c.Storage.UseSSL {
		ve.Warnings = append(ve.Warnings, "storage.use_ssl is disabled — use SSL for production storage connections")
	}

	insecureDBPasswords := []string{"streamgate_password", "streamgate_dev_password"}
	for _, p := range insecureDBPasswords {
		if c.Database.Password == p {
			ve.Critical = append(ve.Critical, fmt.Sprintf("database.password uses insecure default '%s'", p))
			break
		}
	}
	if c.Database.SSLMode == "disable" {
		ve.Warnings = append(ve.Warnings, "database.sslmode is 'disable'")
	}

	if strings.Contains(c.Web3.EthereumRPC, "YOUR_KEY") {
		ve.Warnings = append(ve.Warnings, "web3.ethereum_rpc contains placeholder YOUR_KEY")
	}

	for _, origin := range c.CORS.AllowedOrigins {
		if origin == "*" {
			ve.Warnings = append(ve.Warnings, "cors.allowed_origins contains wildcard '*' — restrict to specific domains")
			break
		}
	}

	if len(ve.Critical) > 0 || len(ve.Warnings) > 0 {
		return &ve
	}
	return nil
}

// DefaultConfig returns a Config populated with sensible defaults.
// This is the canonical default configuration for StreamGate.
func DefaultConfig() *Config {
	return &Config{
		AppName: "streamgate",
		Mode:    "monolith",
		Port:    8080,
		Debug:   false,

		Server: ServerConfig{
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
		},

		GRPC: GRPCConfig{
			Port: 9090,
		},

		Consul: ConsulConfig{
			Address: "localhost",
			Port:    8500,
		},

		Database: DatabaseConfig{
			Host:            "localhost",
			Port:            5432,
			User:            "postgres",
			Password:        envOr("STREAMGATE_DB_PASSWORD", "postgres"),
			Database:        "streamgate",
			SSLMode:         "disable",
			MaxConns:        100,
			MaxIdleConns:    5,
			ConnMaxLifetime: "5m",
		},

		Redis: RedisConfig{
			Host:     "localhost",
			Port:     6379,
			Password: "",
			DB:       0,
			PoolSize: 100,
		},

		Storage: StorageConfig{
			Type:      "minio",
			Endpoint:  "localhost:9000",
			AccessKey: envOr("STREAMGATE_STORAGE_ACCESS_KEY", "minioadmin"),
			SecretKey: envOr("STREAMGATE_STORAGE_SECRET_KEY", "minioadmin"),
			Bucket:    "streamgate",
			Region:    "us-east-1",
			UseSSL:    false,
		},

		NATS: NATSConfig{
			URL: "nats://localhost:4222",
		},

		Web3: Web3Config{
			EthereumRPC: envOr("STREAMGATE_ETH_RPC", "https://sepolia.infura.io/v3/YOUR_KEY"),
			SolanaRPC:   "https://api.devnet.solana.com",
			ChainID:     11155111,
			BlockTag:    "safe",
			Transaction: TransactionConfig{
				GasMultiplier:            1.2,
				Confirmations:            2,
				MaxFeePerGasCapGwei:      500,
				MaxPriorityFeePerGasGwei: 2,
				EIP1559:                  true,
			},
			RateLimit: RPCRateLimitConfig{
				Enabled: true,
				Rate:    10,
				Burst:   20,
			},
		},

		Monitoring: MonitoringConfig{
			PrometheusPort: 9090,
			JaegerEndpoint: "localhost:4317",
			LogLevel:       "info",
		},

		Transcoding: TranscodingConfig{
			Enabled:       true,
			MaxWorkers:    4,
			QueueSize:     100,
			OutputFormats: []string{"hls", "dash"},
		},

		Streaming: StreamingConfig{
			HLSSegmentDuration:   10,
			DASHSegmentDuration:  10,
			CacheEnabled:         true,
			CacheTTL:             "3600s",
			MaxConcurrentStreams: 1000,
		},

		RateLimiting: RateLimitingConfig{
			Enabled:           true,
			RequestsPerMinute: 60,
			RequestsPerHour:   1000,
			BurstSize:         10,
		},

		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Timeout:          "30s",
			MaxRequests:      3,
			WindowTime:       "1m",
		},

		Logging: LoggingConfig{
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			File:       "",
			MaxSize:    100,
			MaxBackups: 3,
			MaxAge:     28,
			Compress:   true,
		},

		Features: FeaturesConfig{
			NFTGating:       true,
			SignatureAuth:   true,
			ChunkedUpload:   true,
			ResumableUpload: true,
			AdaptiveBitrate: true,
			MultiCodec:      true,
		},

		Auth: AuthConfig{
			JWTSecret:          envOr("STREAMGATE_JWT_SECRET", ""),
			JWTExpiry:          "2h",
			RefreshTokenExpiry: "168h",
			NonceExpiry:        "5m",
		},

		CORS: CORSConfig{
			AllowedOrigins: []string{},
		},

		Plugins: PluginsConfig{
			Enabled: []string{},
		},
	}
}

// envOr returns the value of the environment variable named by key,
// or the provided fallback value if the variable is not set or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ConfigChangeHandler is called when configuration changes are detected.
type ConfigChangeHandler func(oldConfig, newConfig *Config) error

// ConfigManager wraps a Config with thread-safe access, file-based
// persistence, hot-reload via file watching, and change handlers.
// This is the canonical config manager for StreamGate.
type ConfigManager struct {
	config       *Config
	configPath   string
	mu           sync.RWMutex
	logger       *zap.Logger
	handlers     []ConfigChangeHandler
	hotReload    bool
	lastModified time.Time
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(configPath string, logger *zap.Logger) *ConfigManager {
	return &ConfigManager{
		configPath: configPath,
		logger:     logger,
		handlers:   make([]ConfigChangeHandler, 0),
		hotReload:  false,
	}
}

// Get returns the current configuration
func (cm *ConfigManager) Get() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// Load loads configuration from the JSON file at configPath
func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config via viper: %w", err)
	}

	oldConfig := cm.config
	cm.config = cfg

	if info, err := os.Stat(cm.configPath); err == nil {
		cm.lastModified = info.ModTime()
	}

	if oldConfig != nil && len(cm.handlers) > 0 {
		for _, handler := range cm.handlers {
			if err := handler(oldConfig, cfg); err != nil {
				cm.logger.Error("Config change handler failed", zap.Error(err))
			}
		}
	}

	return nil
}

// Save saves the current configuration to the JSON file at configPath
func (cm *ConfigManager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.config == nil {
		return fmt.Errorf("no configuration to save")
	}

	data, err := yaml.Marshal(cm.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if info, err := os.Stat(cm.configPath); err == nil {
		cm.lastModified = info.ModTime()
	}

	return nil
}

// Validate validates the current configuration
func (cm *ConfigManager) Validate() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return fmt.Errorf("configuration is not loaded")
	}

	if cm.config.Server.Port <= 0 || cm.config.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", cm.config.Server.Port)
	}

	if cm.config.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}

	return nil
}

// Update updates the configuration and notifies change handlers
func (cm *ConfigManager) Update(newConfig *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	oldConfig := cm.config
	cm.config = newConfig

	if oldConfig != nil && len(cm.handlers) > 0 {
		for _, handler := range cm.handlers {
			if err := handler(oldConfig, newConfig); err != nil {
				cm.logger.Error("Config change handler failed", zap.Error(err))
			}
		}
	}

	return nil
}

// AddChangeHandler adds a handler for configuration changes and returns its index.
func (cm *ConfigManager) AddChangeHandler(handler ConfigChangeHandler) int {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.handlers = append(cm.handlers, handler)
	return len(cm.handlers) - 1
}

// RemoveChangeHandler removes a configuration change handler by index.
func (cm *ConfigManager) RemoveChangeHandler(handlerIndex int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if handlerIndex < 0 || handlerIndex >= len(cm.handlers) {
		return
	}
	cm.handlers = append(cm.handlers[:handlerIndex], cm.handlers[handlerIndex+1:]...)
}

// SetHotReload enables or disables hot reload
func (cm *ConfigManager) SetHotReload(enabled bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.hotReload = enabled
}

// IsHotReloadEnabled returns whether hot reload is enabled
func (cm *ConfigManager) IsHotReloadEnabled() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.hotReload
}

// LoadOrCreate loads configuration from file or creates a default
func LoadOrCreate(configPath string, logger *zap.Logger) (*ConfigManager, error) {
	cm := NewConfigManager(configPath, logger)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Info("Configuration file not found, using defaults", zap.String("path", configPath))
		cm.config = DefaultConfig()

		dir := filepath.Dir(configPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}
	} else {
		// File exists — try Viper-based loading
		cfg, err := LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		cm.config = cfg
	}

	if info, err := os.Stat(configPath); err == nil {
		cm.lastModified = info.ModTime()
	}

	return cm, nil
}

// Watch watches for configuration file changes and reloads automatically
func (cm *ConfigManager) Watch(ctx context.Context, interval time.Duration) error {
	cm.logger.Info("Starting configuration watcher",
		zap.String("path", cm.configPath),
		zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, err := os.Stat(cm.configPath)
			if err != nil {
				continue
			}

			cm.mu.RLock()
			lastMod := cm.lastModified
			cm.mu.RUnlock()

			if info.ModTime().After(lastMod) {
				cm.logger.Info("Configuration file changed, reloading")
				cfg, err := LoadConfig()
				if err != nil {
					cm.logger.Error("Failed to reload configuration", zap.Error(err))
					continue
				}
				_ = cm.Update(cfg)
				cm.mu.Lock()
				cm.lastModified = info.ModTime()
				cm.mu.Unlock()
			}
		}
	}
}
