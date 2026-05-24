package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockServerLifecycle struct {
	started bool
	healthy bool
	startErr error
	stopErr  error
	healthErr error
}

func (s *mockServerLifecycle) Start(ctx context.Context) error {
	if s.startErr != nil {
		return s.startErr
	}
	s.started = true
	return nil
}

func (s *mockServerLifecycle) Stop(ctx context.Context) error {
	if s.stopErr != nil {
		return s.stopErr
	}
	s.started = false
	return nil
}

func (s *mockServerLifecycle) Health(ctx context.Context) error {
	return s.healthErr
}

func TestNewGenericPlugin(t *testing.T) {
	cfg := &config.Config{Mode: "monolith", Server: config.ServerConfig{Port: 8080}}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	p := NewGenericPlugin("test-plugin", cfg, logger, initFn)

	assert.Equal(t, "test-plugin", p.Name())
	assert.Equal(t, "1.0.0", p.Version())
	assert.Nil(t, p.DependsOn())
}

func TestNewGenericPlugin_WithVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	p := NewGenericPlugin("test-plugin", cfg, logger, initFn, WithVersion("2.5.0"))

	assert.Equal(t, "2.5.0", p.Version())
}

func TestNewGenericPluginWithDeps(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	p := NewGenericPluginWithDeps("dep-plugin", cfg, logger, []string{"auth", "cache"}, initFn)

	assert.Equal(t, "dep-plugin", p.Name())
	deps := p.DependsOn()
	assert.Equal(t, []string{"auth", "cache"}, deps)
}

func TestNewGenericPluginWithDeps_EmptyDeps(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	p := NewGenericPluginWithDeps("nodep", cfg, logger, nil, initFn)

	assert.Nil(t, p.DependsOn())
}

func TestGenericPlugin_DependsOn_ReturnsCopy(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	originalDeps := []string{"a", "b"}
	p := NewGenericPluginWithDeps("copy-test", cfg, logger, originalDeps, initFn)

	d1 := p.DependsOn()
	d2 := p.DependsOn()
	assert.Equal(t, d1, d2)
	assert.False(t, &d1[0] == &d2[0], "returned slices should be different copies")
}

func TestGenericPlugin_Init(t *testing.T) {
	cfg := &config.Config{Mode: "monolith", Server: config.ServerConfig{Port: 8080}}
	logger := zap.NewNop()
	server := &mockServerLifecycle{}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("init-test", cfg, logger, initFn)
	kernel := newTestKernel(t)

	err := p.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, p.server)
	assert.Equal(t, kernel, p.kernel)
}

func TestGenericPlugin_InitFailure(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return nil, fmt.Errorf("server creation failed")
	}

	p := NewGenericPlugin("init-fail", cfg, logger, initFn)
	kernel := newTestKernel(t)

	err := p.Init(context.Background(), kernel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create init-fail server")
	assert.Nil(t, p.server)
}

func TestGenericPlugin_Start(t *testing.T) {
	cfg := &config.Config{Mode: "monolith", Server: config.ServerConfig{Port: 8080}}
	logger := zap.NewNop()
	server := &mockServerLifecycle{}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("start-test", cfg, logger, initFn)
	kernel := newTestKernel(t)
	require.NoError(t, p.Init(context.Background(), kernel))

	err := p.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, server.started)
}

func TestGenericPlugin_StartFailure(t *testing.T) {
	cfg := &config.Config{Mode: "monolith", Server: config.ServerConfig{Port: 8080}}
	logger := zap.NewNop()
	server := &mockServerLifecycle{startErr: fmt.Errorf("bind failed")}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("start-fail", cfg, logger, initFn)
	kernel := newTestKernel(t)
	require.NoError(t, p.Init(context.Background(), kernel))

	err := p.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start start-fail server")
}

func TestGenericPlugin_Stop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith", Server: config.ServerConfig{Port: 8080}}
	logger := zap.NewNop()
	server := &mockServerLifecycle{}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("stop-test", cfg, logger, initFn)
	kernel := newTestKernel(t)
	require.NoError(t, p.Init(context.Background(), kernel))
	require.NoError(t, p.Start(context.Background()))

	err := p.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, server.started)
}

func TestGenericPlugin_StopFailure(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	server := &mockServerLifecycle{stopErr: fmt.Errorf("shutdown error")}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("stop-fail", cfg, logger, initFn)
	kernel := newTestKernel(t)
	require.NoError(t, p.Init(context.Background(), kernel))

	err := p.Stop(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stop stop-fail server")
}

func TestGenericPlugin_Stop_NilServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	p := NewGenericPlugin("nil-server", cfg, logger, initFn)

	err := p.Stop(context.Background())
	assert.NoError(t, err)
}

func TestGenericPlugin_Health(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	server := &mockServerLifecycle{}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("health-test", cfg, logger, initFn)
	kernel := newTestKernel(t)
	require.NoError(t, p.Init(context.Background(), kernel))

	err := p.Health(context.Background())
	assert.NoError(t, err)
}

func TestGenericPlugin_Health_Unhealthy(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	server := &mockServerLifecycle{healthErr: fmt.Errorf("db unreachable")}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("unhealthy", cfg, logger, initFn)
	kernel := newTestKernel(t)
	require.NoError(t, p.Init(context.Background(), kernel))

	err := p.Health(context.Background())
	assert.Error(t, err)
	assert.Equal(t, "db unreachable", err.Error())
}

func TestGenericPlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	p := NewGenericPlugin("not-started", cfg, logger, initFn)

	err := p.Health(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestGenericPlugin_FullLifecycle(t *testing.T) {
	cfg := &config.Config{Mode: "monolith", Server: config.ServerConfig{Port: 8080}}
	logger := zap.NewNop()
	server := &mockServerLifecycle{}
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return server, nil
	}

	p := NewGenericPlugin("lifecycle", cfg, logger, initFn, WithVersion("3.0.0"))
	kernel := newTestKernel(t)

	require.NoError(t, p.Init(context.Background(), kernel))
	assert.True(t, server.started == false)

	require.NoError(t, p.Start(context.Background()))
	assert.True(t, server.started)

	require.NoError(t, p.Health(context.Background()))

	require.NoError(t, p.Stop(context.Background()))
	assert.False(t, server.started)
}

func TestNewGenericPluginWithDeps_WithOpts(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	logger := zap.NewNop()
	initFn := func(kernel *Microkernel) (ServerLifecycle, error) {
		return &mockServerLifecycle{}, nil
	}

	p := NewGenericPluginWithDeps("opt-plugin", cfg, logger, []string{"auth"}, initFn, WithVersion("4.5.0"))

	assert.Equal(t, "opt-plugin", p.Name())
	assert.Equal(t, "4.5.0", p.Version())
	deps := p.DependsOn()
	assert.Equal(t, []string{"auth"}, deps)
}
