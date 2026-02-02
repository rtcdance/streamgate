package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Config represents the application configuration
type Config struct {
	Version     string                 `json:"version"`
	Environment string                 `json:"environment"`
	Server      ServerConfig           `json:"server"`
	Database    DatabaseConfig         `json:"database"`
	Redis       RedisConfig            `json:"redis"`
	Storage     StorageConfig          `json:"storage"`
	Web3        Web3Config             `json:"web3"`
	Plugins     map[string]interface{} `json:"plugins"`
	Metrics     MetricsConfig          `json:"metrics"`
	Tracing     TracingConfig          `json:"tracing"`
	Security    SecurityConfig         `json:"security"`
	Custom      map[string]interface{} `json:"custom"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Host         string        `json:"host"`
	Port         int           `json:"port"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"`
	Mode         string        `json:"mode"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	User            string        `json:"user"`
	Password        string        `json:"password"`
	Database        string        `json:"database"`
	SSLMode         string        `json:"ssl_mode"`
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"`
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	Password string        `json:"password"`
	DB       int           `json:"db"`
	PoolSize int           `json:"pool_size"`
	Timeout  time.Duration `json:"timeout"`
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	Type      string `json:"type"`
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	UseSSL    bool   `json:"use_ssl"`
}

// Web3Config holds Web3 configuration
type Web3Config struct {
	Ethereum EthereumConfig `json:"ethereum"`
	Solana   SolanaConfig   `json:"solana"`
}

// EthereumConfig holds Ethereum configuration
type EthereumConfig struct {
	RPCEndpoint string `json:"rpc_endpoint"`
	ChainID     int64  `json:"chain_id"`
}

// SolanaConfig holds Solana configuration
type SolanaConfig struct {
	RPCEndpoint string `json:"rpc_endpoint"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled   bool   `json:"enabled"`
	Port      int    `json:"port"`
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
}

// TracingConfig holds tracing configuration
type TracingConfig struct {
	Enabled  bool    `json:"enabled"`
	Endpoint string  `json:"endpoint"`
	Sampler  float64 `json:"sampler"`
	Service  string  `json:"service"`
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	JWTSecret          string          `json:"jwt_secret"`
	TokenExpiry        time.Duration   `json:"token_expiry"`
	RefreshTokenExpiry time.Duration   `json:"refresh_token_expiry"`
	RateLimit          RateLimitConfig `json:"rate_limit"`
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	Enabled  bool          `json:"enabled"`
	Requests int           `json:"requests"`
	Window   time.Duration `json:"window"`
}

// ConfigChangeHandler handles configuration changes
type ConfigChangeHandler func(oldConfig, newConfig *Config) error

// ConfigManager manages application configuration
type ConfigManager struct {
	config       *Config
	configPath   string
	mu           sync.RWMutex
	logger       *zap.Logger
	handlers     []ConfigChangeHandler
	hotReload    bool
	watcher      *fileWatcher
	lastModified time.Time
}

// fileWatcher watches for file changes
type fileWatcher struct {
	path    string
	changed chan struct{}
	stop    chan struct{}
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

// Load loads configuration from file
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

	info, err := os.Stat(cm.configPath)
	if err == nil {
		cm.lastModified = info.ModTime()
	}

	cm.logger.Info("Configuration loaded",
		zap.String("path", cm.configPath),
		zap.String("version", newConfig.Version),
		zap.String("environment", newConfig.Environment))

	if oldConfig != nil && len(cm.handlers) > 0 {
		for _, handler := range cm.handlers {
			if err := handler(oldConfig, &newConfig); err != nil {
				cm.logger.Error("Config change handler failed", zap.Error(err))
			}
		}
	}

	return nil
}

// Save saves configuration to file
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

	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	info, err := os.Stat(cm.configPath)
	if err == nil {
		cm.lastModified = info.ModTime()
	}

	cm.logger.Info("Configuration saved", zap.String("path", cm.configPath))

	return nil
}

// Get returns the current configuration
func (cm *ConfigManager) Get() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.config
}

// Update updates the configuration
func (cm *ConfigManager) Update(newConfig *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	oldConfig := cm.config
	cm.config = newConfig

	cm.logger.Info("Configuration updated")

	if oldConfig != nil && len(cm.handlers) > 0 {
		for _, handler := range cm.handlers {
			if err := handler(oldConfig, newConfig); err != nil {
				cm.logger.Error("Config change handler failed", zap.Error(err))
			}
		}
	}

	return nil
}

// Reload reloads the configuration from file
func (cm *ConfigManager) Reload() error {
	return cm.Load()
}

// Watch starts watching for configuration changes
func (cm *ConfigManager) Watch(ctx context.Context, interval time.Duration) error {
	cm.mu.Lock()
	if cm.watcher != nil {
		cm.mu.Unlock()
		return fmt.Errorf("already watching for changes")
	}

	cm.watcher = &fileWatcher{
		path:    cm.configPath,
		changed: make(chan struct{}, 1),
		stop:    make(chan struct{}),
	}
	cm.mu.Unlock()

	cm.logger.Info("Starting configuration watcher",
		zap.String("path", cm.configPath),
		zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cm.mu.Lock()
			if cm.watcher != nil {
				close(cm.watcher.stop)
				cm.watcher = nil
			}
			cm.mu.Unlock()
			return nil

		case <-ticker.C:
			info, err := os.Stat(cm.configPath)
			if err != nil {
				cm.logger.Error("Failed to stat config file", zap.Error(err))
				continue
			}

			cm.mu.RLock()
			lastMod := cm.lastModified
			cm.mu.RUnlock()

			if info.ModTime().After(lastMod) {
				cm.logger.Info("Configuration file changed, reloading")
				if err := cm.Reload(); err != nil {
					cm.logger.Error("Failed to reload configuration", zap.Error(err))
				}
			}
		}
	}
}

// StopWatching stops watching for configuration changes
func (cm *ConfigManager) StopWatching() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.watcher != nil {
		close(cm.watcher.stop)
		cm.watcher = nil
		cm.logger.Info("Configuration watcher stopped")
	}
}

// AddChangeHandler adds a handler for configuration changes
func (cm *ConfigManager) AddChangeHandler(handler ConfigChangeHandler) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.handlers = append(cm.handlers, handler)
	cm.logger.Info("Configuration change handler added")
}

// RemoveChangeHandler removes a configuration change handler
func (cm *ConfigManager) RemoveChangeHandler(handler ConfigChangeHandler) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for i, h := range cm.handlers {
		if &h == &handler {
			cm.handlers = append(cm.handlers[:i], cm.handlers[i+1:]...)
			break
		}
	}
	cm.logger.Info("Configuration change handler removed")
}

// SetHotReload enables or disables hot reload
func (cm *ConfigManager) SetHotReload(enabled bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.hotReload = enabled
	cm.logger.Info("Hot reload setting changed", zap.Bool("enabled", enabled))
}

// IsHotReloadEnabled returns whether hot reload is enabled
func (cm *ConfigManager) IsHotReloadEnabled() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.hotReload
}

// GetServerConfig returns the server configuration
func (cm *ConfigManager) GetServerConfig() ServerConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return ServerConfig{}
	}
	return cm.config.Server
}

// GetDatabaseConfig returns the database configuration
func (cm *ConfigManager) GetDatabaseConfig() DatabaseConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return DatabaseConfig{}
	}
	return cm.config.Database
}

// GetRedisConfig returns the Redis configuration
func (cm *ConfigManager) GetRedisConfig() RedisConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return RedisConfig{}
	}
	return cm.config.Redis
}

// GetStorageConfig returns the storage configuration
func (cm *ConfigManager) GetStorageConfig() StorageConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return StorageConfig{}
	}
	return cm.config.Storage
}

// GetWeb3Config returns the Web3 configuration
func (cm *ConfigManager) GetWeb3Config() Web3Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return Web3Config{}
	}
	return cm.config.Web3
}

// GetMetricsConfig returns the metrics configuration
func (cm *ConfigManager) GetMetricsConfig() MetricsConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return MetricsConfig{}
	}
	return cm.config.Metrics
}

// GetTracingConfig returns the tracing configuration
func (cm *ConfigManager) GetTracingConfig() TracingConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return TracingConfig{}
	}
	return cm.config.Tracing
}

// GetSecurityConfig returns the security configuration
func (cm *ConfigManager) GetSecurityConfig() SecurityConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return SecurityConfig{}
	}
	return cm.config.Security
}

// GetCustomConfig returns custom configuration values
func (cm *ConfigManager) GetCustomConfig() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return nil
	}
	return cm.config.Custom
}

// GetCustomValue returns a custom configuration value
func (cm *ConfigManager) GetCustomValue(key string) (interface{}, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil || cm.config.Custom == nil {
		return nil, false
	}

	val, ok := cm.config.Custom[key]
	return val, ok
}

// SetCustomValue sets a custom configuration value
func (cm *ConfigManager) SetCustomValue(key string, value interface{}) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.config == nil {
		return
	}

	if cm.config.Custom == nil {
		cm.config.Custom = make(map[string]interface{})
	}

	cm.config.Custom[key] = value
}

// GetPluginConfig returns plugin configuration
func (cm *ConfigManager) GetPluginConfig(pluginName string) (map[string]interface{}, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil || cm.config.Plugins == nil {
		return nil, false
	}

	val, exists := cm.config.Plugins[pluginName]
	if config, ok := val.(map[string]interface{}); ok {
		return config, exists
	}

	return nil, false
}

// SetPluginConfig sets plugin configuration
func (cm *ConfigManager) SetPluginConfig(pluginName string, config map[string]interface{}) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.config == nil {
		return
	}

	if cm.config.Plugins == nil {
		cm.config.Plugins = make(map[string]interface{})
	}

	cm.config.Plugins[pluginName] = config
}

// Validate validates the configuration
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

	if cm.config.Database.Port <= 0 || cm.config.Database.Port > 65535 {
		return fmt.Errorf("invalid database port: %d", cm.config.Database.Port)
	}

	if cm.config.Redis.Host == "" {
		return fmt.Errorf("redis host is required")
	}

	if cm.config.Storage.Type == "" {
		return fmt.Errorf("storage type is required")
	}

	return nil
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Version:     "1.0.0",
		Environment: "development",
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
			Mode:         "debug",
		},
		Database: DatabaseConfig{
			Host:            "localhost",
			Port:            5432,
			User:            "postgres",
			Password:        "postgres",
			Database:        "streamgate",
			SSLMode:         "disable",
			MaxOpenConns:    25,
			MaxIdleConns:    5,
			ConnMaxLifetime: 30 * time.Minute,
			ConnMaxIdleTime: 5 * time.Minute,
		},
		Redis: RedisConfig{
			Host:     "localhost",
			Port:     6379,
			Password: "",
			DB:       0,
			PoolSize: 10,
			Timeout:  5 * time.Second,
		},
		Storage: StorageConfig{
			Type:      "minio",
			Endpoint:  "localhost:9000",
			AccessKey: "minioadmin",
			SecretKey: "minioadmin",
			Bucket:    "streamgate",
			Region:    "us-east-1",
			UseSSL:    false,
		},
		Web3: Web3Config{
			Ethereum: EthereumConfig{
				RPCEndpoint: "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
				ChainID:     1,
			},
			Solana: SolanaConfig{
				RPCEndpoint: "https://api.mainnet-beta.solana.com",
			},
		},
		Metrics: MetricsConfig{
			Enabled:   true,
			Port:      9090,
			Path:      "/metrics",
			Namespace: "streamgate",
		},
		Tracing: TracingConfig{
			Enabled:  false,
			Endpoint: "http://localhost:4318",
			Sampler:  1.0,
			Service:  "streamgate",
		},
		Security: SecurityConfig{
			JWTSecret:          "change-me-in-production",
			TokenExpiry:        24 * time.Hour,
			RefreshTokenExpiry: 7 * 24 * time.Hour,
			RateLimit: RateLimitConfig{
				Enabled:  true,
				Requests: 100,
				Window:   time.Minute,
			},
		},
		Plugins: make(map[string]interface{}),
		Custom:  make(map[string]interface{}),
	}
}

// LoadOrCreate loads configuration or creates default
func LoadOrCreate(configPath string, logger *zap.Logger) (*ConfigManager, error) {
	cm := NewConfigManager(configPath, logger)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Info("Configuration file not found, creating default", zap.String("path", configPath))
		cm.config = DefaultConfig()

		dir := filepath.Dir(configPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}

		if err := cm.Save(); err != nil {
			return nil, fmt.Errorf("failed to save default config: %w", err)
		}
	} else {
		if err := cm.Load(); err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
	}

	return cm, nil
}

// Merge merges two configurations
func Merge(base, override *Config) *Config {
	result := *base

	if override.Server.Host != "" {
		result.Server = override.Server
	}

	if override.Database.Host != "" {
		result.Database = override.Database
	}

	if override.Redis.Host != "" {
		result.Redis = override.Redis
	}

	if override.Storage.Type != "" {
		result.Storage = override.Storage
	}

	if override.Web3.Ethereum.RPCEndpoint != "" {
		result.Web3 = override.Web3
	}

	if override.Metrics.Enabled {
		result.Metrics = override.Metrics
	}

	if override.Tracing.Enabled {
		result.Tracing = override.Tracing
	}

	if override.Security.JWTSecret != "" {
		result.Security = override.Security
	}

	for k, v := range override.Plugins {
		if result.Plugins == nil {
			result.Plugins = make(map[string]interface{})
		}
		result.Plugins[k] = v
	}

	for k, v := range override.Custom {
		if result.Custom == nil {
			result.Custom = make(map[string]interface{})
		}
		result.Custom[k] = v
	}

	return &result
}
