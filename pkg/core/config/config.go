package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
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

	// Web3
	Web3 Web3Config

	// Monitoring
	Monitoring MonitoringConfig

	// Auth
	Auth AuthConfig

	// CORS
	CORS CORSConfig

	// Plugins (for monolithic mode)
	Plugins PluginsConfig
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port         int
	ReadTimeout  int
	WriteTimeout int
}

// GRPCConfig holds gRPC configuration
type GRPCConfig struct {
	Port int
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
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
	MaxConns int
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
	Transaction   TransactionConfig
	RateLimit     RPCRateLimitConfig
}

// RPCRateLimitConfig holds RPC rate limiting configuration
type RPCRateLimitConfig struct {
	Enabled bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	Rate    float64 `mapstructure:"rate" json:"rate" yaml:"rate"`   // requests per second per RPC endpoint
	Burst   float64 `mapstructure:"burst" json:"burst" yaml:"burst"` // max burst size
}

// TransactionConfig holds on-chain transaction parameters
type TransactionConfig struct {
	PrivateKeyHex          string  // hex-encoded private key for signing
	GasLimit               uint64  // default gas limit
	GasMultiplier          float64 // multiplier on estimated gas (e.g. 1.2 for 20% buffer)
	Confirmations          uint64  // number of block confirmations to wait
	MaxFeePerGasGwei       float64 // max fee per gas in Gwei (EIP-1559 floor)
	MaxFeePerGasCapGwei    float64 // hard cap on max fee per gas in Gwei (safety limit, default 500)
	MaxPriorityFeePerGasGwei float64 // max priority fee per gas in Gwei (EIP-1559 tip)
	EIP1559                bool    // use EIP-1559 dynamic fee transactions when true
}

// MonitoringConfig holds monitoring configuration
type MonitoringConfig struct {
	PrometheusPort int
	JaegerEndpoint string
	LogLevel       string
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWTSecret   string
	NonceExpiry string
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins []string
}

// LoadConfig loads configuration from environment and config files
func LoadConfig() (*Config, error) {
	viper.SetConfigName("config")
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

	// Read config file if it exists
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found; using environment variables
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
			Host:     viper.GetString("database.host"),
			Port:     viper.GetInt("database.port"),
			User:     viper.GetString("database.user"),
			Password: viper.GetString("database.password"),
			Database: viper.GetString("database.database"),
			SSLMode:  viper.GetString("database.sslmode"),
			MaxConns: viper.GetInt("database.maxconns"),
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
		},

		NATS: NATSConfig{
			URL: viper.GetString("nats.url"),
		},

		Web3: Web3Config{
			EthereumRPC: viper.GetString("web3.ethereum_rpc"),
			SolanaRPC:   viper.GetString("web3.solana_rpc"),
			ChainID:     viper.GetInt64("web3.chain_id"),
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

		Auth: AuthConfig{
			JWTSecret:   viper.GetString("auth.jwt_secret"),
			NonceExpiry: viper.GetString("auth.nonce_expiry"),
		},

		CORS: CORSConfig{
			AllowedOrigins: viper.GetStringSlice("cors.allowed_origins"),
		},

		Plugins: PluginsConfig{
			Enabled: viper.GetStringSlice("plugins.enabled"),
		},
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

	// NATS defaults
	viper.SetDefault("nats.url", "nats://localhost:4222")

	// Web3 defaults
	viper.SetDefault("web3.ethereum_rpc", "https://sepolia.infura.io/v3/YOUR_KEY")
	viper.SetDefault("web3.solana_rpc", "https://api.devnet.solana.com")
	viper.SetDefault("web3.chain_id", 11155111) // Sepolia

	// Monitoring defaults
	viper.SetDefault("monitoring.prometheus_port", 9090)
	viper.SetDefault("monitoring.jaeger_endpoint", "http://localhost:14268/api/traces")
	viper.SetDefault("monitoring.log_level", "info")

	// Auth defaults
	viper.SetDefault("auth.jwt_secret", "streamgate-dev-secret")
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

// ValidateProduction checks for insecure default values that should not be
// present in a production deployment. Security checks apply regardless of
// deployment mode (monolith or microservice).
func (c *Config) ValidateProduction(log *zap.Logger) error {
	var violations []string

	// JWT secret checks
	if c.Auth.JWTSecret == "" {
		violations = append(violations, "auth.jwt_secret is empty — set via AUTH_JWT_SECRET env var")
	}
	insecureSecrets := []string{
		"streamgate-dev-secret",
		"your-secret-key-change-in-production",
		"dev-secret-key-not-for-production",
	}
	for _, s := range insecureSecrets {
		if c.Auth.JWTSecret == s {
			violations = append(violations, fmt.Sprintf("auth.jwt_secret uses insecure default '%s'", s))
			break
		}
	}

	// Storage credential checks
	if c.Storage.AccessKey == "minioadmin" || c.Storage.SecretKey == "minioadmin" {
		violations = append(violations, "storage credentials use dev default 'minioadmin'")
	}

	// Database checks
	insecureDBPasswords := []string{"streamgate_password", "streamgate_dev_password"}
	for _, p := range insecureDBPasswords {
		if c.Database.Password == p {
			violations = append(violations, fmt.Sprintf("database.password uses insecure default '%s'", p))
			break
		}
	}
	if c.Database.SSLMode == "disable" {
		violations = append(violations, "database.sslmode is 'disable'")
	}

	// Web3 RPC placeholder check
	if strings.Contains(c.Web3.EthereumRPC, "YOUR_KEY") {
		violations = append(violations, "web3.ethereum_rpc contains placeholder YOUR_KEY")
	}

	// CORS wildcard check
	for _, origin := range c.CORS.AllowedOrigins {
		if origin == "*" {
			violations = append(violations, "cors.allowed_origins contains wildcard '*' — restrict to specific domains")
			break
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("production config validation failed — insecure defaults: %s", strings.Join(violations, "; "))
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
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: envOr("STREAMGATE_DB_PASSWORD", "postgres"),
			Database: "streamgate",
			SSLMode:  "disable",
			MaxConns: 100,
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
		},

		NATS: NATSConfig{
			URL: "nats://localhost:4222",
		},

		Web3: Web3Config{
			EthereumRPC: envOr("STREAMGATE_ETH_RPC", "https://sepolia.infura.io/v3/YOUR_KEY"),
			SolanaRPC:   "https://api.devnet.solana.com",
			ChainID:     11155111,
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
			JaegerEndpoint: "http://localhost:14268/api/traces",
			LogLevel:       "info",
		},

		Auth: AuthConfig{
			JWTSecret:   "streamgate-dev-secret",
			NonceExpiry: "5m",
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

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var newConfig Config
	if err := json.Unmarshal(data, &newConfig); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	oldConfig := cm.config
	cm.config = &newConfig

	if info, err := os.Stat(cm.configPath); err == nil {
		cm.lastModified = info.ModTime()
	}

	if oldConfig != nil && len(cm.handlers) > 0 {
		for _, handler := range cm.handlers {
			if err := handler(oldConfig, &newConfig); err != nil {
				cm.logger.Error("Config change handler failed", zap.Error(err))
			}
		}
	}

	return nil
}

// Save saves the current configuration to the JSON file at configPath
func (cm *ConfigManager) Save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return fmt.Errorf("no configuration to save")
	}

	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0o644); err != nil {
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
