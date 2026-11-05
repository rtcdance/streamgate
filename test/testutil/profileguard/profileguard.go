package profileguard

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

const helperKey = "STREAMGATE_LOCAL_PROFILE_PROBE"

// PassConfigGuard exercises the real main, then stops at its configuration
// boundary. This probe cannot establish dependency or runtime readiness.
func PassConfigGuard(t *testing.T, service string, entrypoint func()) {
	t.Helper()
	if os.Getenv(helperKey) == service+":"+t.Name() {
		entrypoint()
		return
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("repository root unavailable")
		}
		root = parent
	}
	raw, err := os.ReadFile(filepath.Join(root, "deploy", "docker", "compose.w3sepolia.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var recipe struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &recipe); err != nil {
		t.Fatal(err)
	}
	environment := recipe.Services[service].Environment
	if len(environment) == 0 {
		t.Fatalf("tracked recipe has no environment for %s", service)
	}
	fixtures := map[string]string{
		"W3_JWT_SECRET":            "config-probe-only-not-a-live-signing-secret-32-bytes",
		"SEPOLIA_RPC_URL":          "https://primary.example",
		"SEPOLIA_RPC_FALLBACK_URL": "https://fallback.example",
	}
	// Expand only Compose's required-input and default forms used by this
	// recipe; unresolved expressions must never reach configuration loading.
	input := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)(?:(:\?|:-)([^}]*))?\}`)
	for key, value := range environment {
		environment[key] = input.ReplaceAllStringFunc(value, func(token string) string {
			match := input.FindStringSubmatch(token)
			name := match[1]
			fixture, ok := fixtures[name]
			if !ok && match[2] == ":-" {
				return match[3]
			}
			if !ok {
				t.Fatalf("missing isolated fixture for %s", name)
			}
			return fixture
		})
		if strings.Contains(environment[key], "${") {
			t.Fatalf("unsupported Compose expression in %s", key)
		}
	}
	// Never let the subprocess inherit real deployment credentials or reach a
	// developer's storage while observing the boundary before provider setup.
	for key, value := range map[string]string{
		"STREAMGATE_DB_HOST": "127.0.0.1", "STREAMGATE_DB_PORT": "1",
		"STREAMGATE_REDIS_HOST": "127.0.0.1", "STREAMGATE_REDIS_PORT": "1",
		"STREAMGATE_STORAGE_ENDPOINT": "127.0.0.1:1", "STREAMGATE_NATS_URL": "nats://127.0.0.1:1",
	} {
		environment[key] = value
	}
	environment[helperKey] = service + ":" + t.Name()
	temporary := t.TempDir()
	if err := os.Mkdir(filepath.Join(temporary, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.yaml", "config.w3sepolia.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, "config", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(temporary, "config", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prefixes := []string{"APP_", "STREAMGATE_", "SEPOLIA_", "WEB3_", "FEATURES_", "AUTH_", "DATABASE_", "DB_", "REDIS_", "NATS_", "STORAGE_", "SIWE_", "W3_", "MINIO_", "POSTGRES_"}
	childEnvironment := make([]string, 0, len(os.Environ())+len(environment))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		blocked := false
		for _, prefix := range prefixes {
			blocked = blocked || strings.HasPrefix(key, prefix)
		}
		if !blocked {
			childEnvironment = append(childEnvironment, item)
		}
	}
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		childEnvironment = append(childEnvironment, key+"="+environment[key])
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$")
	command.Env, command.Dir = childEnvironment, temporary
	log := &boundaryLog{ready: make(chan struct{})}
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	select {
	case <-log.ready:
		_ = command.Process.Kill()
		<-finished
	case <-finished:
	case <-ctx.Done():
		<-finished
	}
	if !strings.Contains(log.String(), "Configuration loaded") {
		t.Fatalf("%s did not pass its real startup config guard:\n%s", service, log.String())
	}
}

type boundaryLog struct {
	sync.Mutex
	buffer bytes.Buffer
	ready  chan struct{}
	once   sync.Once
}

func (l *boundaryLog) Write(data []byte) (int, error) {
	l.Lock()
	defer l.Unlock()
	if l.buffer.Len() < 128*1024 {
		_, _ = l.buffer.Write(data)
	}
	if bytes.Contains(l.buffer.Bytes(), []byte("Configuration loaded")) {
		l.once.Do(func() { close(l.ready) })
	}
	return len(data), nil
}

func (l *boundaryLog) String() string {
	l.Lock()
	defer l.Unlock()
	return l.buffer.String()
}
