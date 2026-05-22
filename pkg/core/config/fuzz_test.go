package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func FuzzValidateProduction(f *testing.F) {
	f.Add("streamgate-dev-secret", "minioadmin", "minioadmin", "disable", "https://sepolia.infura.io/v3/YOUR_KEY")
	f.Add("", "", "", "", "")
	f.Add("short", "", "", "", "not_a_url")

	cfg := &Config{
		AppName: "streamgate",
		Mode:    "monolith",
		Port:    8080,
	}

	f.Fuzz(func(t *testing.T, jwtSecret, accessKey, secretKey, sslMode, rpcURL string) {
		cfg.Auth = AuthConfig{JWTSecret: jwtSecret}
		cfg.Storage = StorageConfig{AccessKey: accessKey, SecretKey: secretKey}
		cfg.Database = DatabaseConfig{SSLMode: sslMode}
		cfg.Web3 = Web3Config{EthereumRPC: rpcURL}
		_ = cfg.ValidateProduction(nil)
	})
}

func FuzzLoadYAMLConfig(f *testing.F) {
	f.Add([]byte("server:\n  port: 8080\nmode: monolith\nauth:\n  jwt_secret: test\n"))
	f.Add([]byte{})
	f.Add([]byte("broken: yaml: \n  [invalid"))

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return
		}
		v := viper.New()
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return
		}
		// Reading arbitrary YAML should never panic
		_ = v.GetString("app.name")
		_ = v.GetString("app.mode")
		_ = v.GetInt("server.port")
	})
}

func FuzzDefaultConfigString(f *testing.F) {
	f.Add("streamgate", "monolith", "8080")
	f.Add("", "", "not_a_number")
	f.Add("a", "b", "-1")

	f.Fuzz(func(t *testing.T, appName, mode, port string) {
		os.Setenv("STREAMGATE_APP_PORT", port)
		cfg := &Config{
			AppName: appName,
			Mode:    mode,
		}
		_ = cfg.ValidateProduction(nil)
		os.Unsetenv("STREAMGATE_APP_PORT")
	})
}
