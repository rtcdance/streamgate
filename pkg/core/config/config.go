package config

import (
	"fmt"

	"github.com/spf13/viper"
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
	EthereumRPC string
	SolanaRPC   string
	ChainID     int64
}

// MonitoringConfig holds monitoring configuration
type MonitoringConfig struct {
	PrometheusPort int
	JaegerEndpoint string
	LogLevel       string
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
	viper.AutomaticEnv()

	// Explicitly bind environment variables for database config
	viper.BindEnv("database.host", "DATABASE_HOST")
	viper.BindEnv("database.port", "DATABASE_PORT")
	viper.BindEnv("database.user", "DATABASE_USER")
	viper.BindEnv("database.password", "DATABASE_PASSWORD")
	viper.BindEnv("database.database", "DATABASE_NAME")
	viper.BindEnv("database.sslmode", "DATABASE_SSLMODE")
	viper.BindEnv("database.maxconns", "DATABASE_MAXCONNS")

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
		},

		Monitoring: MonitoringConfig{
			PrometheusPort: viper.GetInt("monitoring.prometheus_port"),
			JaegerEndpoint: viper.GetString("monitoring.jaeger_endpoint"),
			LogLevel:       viper.GetString("monitoring.log_level"),
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
