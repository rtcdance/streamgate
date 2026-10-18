package barrier

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func durableConfig(dir string, timeout time.Duration) *config.Config {
	return &config.Config{
		Profile: config.ProfileDurable,
		Acceptance: config.AcceptanceConfig{
			FaultBarrier:        true,
			FaultBarrierDir:     dir,
			FaultBarrierTimeout: timeout,
		},
	}
}

func TestLoad_DisabledIsNilAndEveryCallIsAnop(t *testing.T) {
	b, err := Load(&config.Config{}, zap.NewNop())
	require.NoError(t, err)
	require.Nil(t, b)

	// The component path must be silent: a disabled barrier cannot fail a worker.
	require.Equal(t, "", b.Dir())
	require.Equal(t, time.Duration(0), b.Timeout())
	require.NoError(t, b.Reset())
	p := b.Point("worker.after_commit")
	require.Nil(t, p)
	require.NoError(t, p.Arrive(context.Background()))

	// The harness path fails loudly instead: asking to release a point that does
	// not exist is a harness bug, not a component no-op.
	require.Error(t, p.Release())
	require.Error(t, p.WaitReached(context.Background(), time.Millisecond))
	require.False(t, p.Reached())
	require.Equal(t, "", p.Name())
}

func TestLoad_RefusesEveryProfileButDurable(t *testing.T) {
	for _, profile := range []string{config.ProfileDemo, config.ProfileUnit, config.ProfileProduction, ""} {
		t.Run("profile="+profile, func(t *testing.T) {
			cfg := durableConfig(t.TempDir(), time.Second)
			cfg.Profile = profile

			b, err := Load(cfg, zap.NewNop())
			require.Error(t, err, "profile %q must not be able to enable the fault barrier", profile)
			require.Nil(t, b)
			assert.Contains(t, err.Error(), config.ProfileDurable)
		})
	}
}

func TestLoad_RefusesAMissingOrRelativeDirectory(t *testing.T) {
	b, err := Load(durableConfig("", time.Second), zap.NewNop())
	require.Error(t, err)
	require.Nil(t, b)

	b, err = Load(durableConfig("relative/rendezvous", time.Second), zap.NewNop())
	require.Error(t, err)
	require.Nil(t, b)
	assert.Contains(t, err.Error(), "absolute")
}

func TestLoad_CreatesTheDirectoryAndDefaultsTheTimeout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rendezvous")
	b, err := Load(durableConfig(dir, 0), zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, dir, b.Dir())
	assert.Equal(t, defaultTimeout, b.Timeout())

	info, statErr := os.Stat(dir)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestArrive_PublishesTheReachedStateThenContinuesOnRelease(t *testing.T) {
	b, err := Load(durableConfig(t.TempDir(), 5*time.Second), zap.NewNop())
	require.NoError(t, err)
	p := b.Point("worker.after_commit")

	done := make(chan error, 1)
	go func() { done <- p.Arrive(context.Background()) }()

	require.NoError(t, p.WaitReached(context.Background(), 3*time.Second),
		"the reached marker must be observable before the fault is injected")
	assert.True(t, p.Reached())
	select {
	case err := <-done:
		t.Fatalf("Arrive returned %v without ever being released", err)
	default:
	}

	require.NoError(t, p.Release())
	require.NoError(t, <-done)
	assert.False(t, p.Reached(), "the reached marker must be withdrawn once the component continues")
	assert.NoFileExists(t, filepath.Join(b.Dir(), "worker.after_commit.release"),
		"the release marker must be consumed, not latched")
}

func TestArrive_IsARendezvousSoTheSamePointServesAgain(t *testing.T) {
	b, err := Load(durableConfig(t.TempDir(), 5*time.Second), zap.NewNop())
	require.NoError(t, err)
	p := b.Point("worker.before_broadcast")

	for round := 1; round <= 2; round++ {
		done := make(chan error, 1)
		go func() { done <- p.Arrive(context.Background()) }()
		require.NoError(t, p.WaitReached(context.Background(), 3*time.Second), "round %d", round)
		require.NoError(t, p.Release())
		require.NoError(t, <-done, "round %d", round)
	}
}

func TestArrive_FailsLoudlyWhenTheScenarioNeverReleases(t *testing.T) {
	b, err := Load(durableConfig(t.TempDir(), 80*time.Millisecond), zap.NewNop())
	require.NoError(t, err)
	p := b.Point("publisher.after_broadcast")

	err = p.Arrive(context.Background())
	require.Error(t, err, "a point that is never released must fail rather than continue silently")
	assert.Contains(t, err.Error(), "no release within")
	assert.False(t, p.Reached(), "the state must not survive the failure as a stale latch")
}

func TestArrive_StopsWaitingWhenTheComponentIsCancelled(t *testing.T) {
	b, err := Load(durableConfig(t.TempDir(), 5*time.Second), zap.NewNop())
	require.NoError(t, err)
	p := b.Point("indexer.checkpoint")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Arrive(ctx) }()
	require.NoError(t, p.WaitReached(context.Background(), 3*time.Second))
	cancel()

	err = <-done
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestWaitReached_ReportsAPointThatNeverArrives(t *testing.T) {
	b, err := Load(durableConfig(t.TempDir(), 5*time.Second), zap.NewNop())
	require.NoError(t, err)

	err = b.Point("worker.never").WaitReached(context.Background(), 60*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not reached within")
}

func TestReset_ClearsMarkersButLeavesForeignFiles(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(durableConfig(dir, time.Second), zap.NewNop())
	require.NoError(t, err)

	keep := filepath.Join(dir, "scenario.json")
	require.NoError(t, os.WriteFile(keep, []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "worker.after_commit.reached"), []byte("stale"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "worker.after_commit.release"), []byte("stale"), 0o644))

	require.NoError(t, b.Reset())
	assert.NoFileExists(t, filepath.Join(dir, "worker.after_commit.reached"))
	assert.NoFileExists(t, filepath.Join(dir, "worker.after_commit.release"))
	assert.FileExists(t, keep, "Reset must only own the markers it writes")
}
