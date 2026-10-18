// Package barrier is the internal fault barrier of the durable acceptance
// stack (TRANSITION_PLAN section 0.2 D).
//
// A scenario must inject a fault at the exact instant a component reaches a
// defined point — kill a worker between its claim and its commit, drop the
// response to a transaction the chain already accepted, fail a checkpoint
// commit. Guessing that instant with a sleep makes the scenario flaky and the
// result unfalsifiable, so instead the component announces the state it reached
// (it writes <dir>/<point>.reached) and waits; the harness observes that marker,
// performs the fault, and then releases the component (<dir>/<point>.release).
//
// The barrier is disabled by default and is refused outside the durable
// profile, so a zero-value handle is a no-op and production traffic can never
// observe it.
package barrier

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"go.uber.org/zap"
)

const (
	reachedSuffix = ".reached"
	releaseSuffix = ".release"
	armedSuffix   = ".armed"
	pollInterval  = 10 * time.Millisecond
	// defaultTimeout bounds a point that the harness never releases. A scenario
	// that did not actually fire must fail loudly; waiting forever would turn it
	// into a hang.
	defaultTimeout = 30 * time.Second
)

// Barrier is the filesystem rendezvous shared by every fault point of one
// process. Every method is nil-safe, so a component holds a handle that is nil
// whenever the barrier is off and pays nothing for it.
type Barrier struct {
	dir     string
	timeout time.Duration
	log     *zap.Logger
}

// Load returns the barrier described by cfg, or nil when the switch is off.
//
// It refuses to engage outside the durable profile: a demo or production
// deployment that carries the switch must fail loudly instead of injecting
// faults into traffic a real user is watching.
func Load(cfg *config.Config, log *zap.Logger) (*Barrier, error) {
	if cfg == nil || !cfg.Acceptance.FaultBarrier {
		return nil, nil
	}
	if profile := cfg.EffectiveProfile(); profile != config.ProfileDurable {
		return nil, fmt.Errorf("the fault barrier may only be enabled under the %q profile (effective profile %q)",
			config.ProfileDurable, profile)
	}
	dir := strings.TrimSpace(cfg.Acceptance.FaultBarrierDir)
	if dir == "" {
		return nil, errors.New("acceptance.fault_barrier_dir is required when the fault barrier is enabled")
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("acceptance.fault_barrier_dir must be absolute, got %q", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create the fault barrier directory %q: %w", dir, err)
	}
	timeout := cfg.Acceptance.FaultBarrierTimeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Barrier{dir: dir, timeout: timeout, log: log}, nil
}

// Dir is the rendezvous directory, or "" when no barrier is configured.
func (b *Barrier) Dir() string {
	if b == nil {
		return ""
	}
	return b.dir
}

// Timeout is the release bound applied to each point.
func (b *Barrier) Timeout() time.Duration {
	if b == nil {
		return 0
	}
	return b.timeout
}

// Point returns a handle to one named fault point. Store it once at
// construction: a nil *Barrier yields a nil *Point, and a nil *Point is a no-op.
func (b *Barrier) Point(name string) *Point {
	if b == nil {
		return nil
	}
	return &Point{barrier: b, name: name}
}

// Point is one named fault point.
type Point struct {
	barrier *Barrier
	name    string
}

// hostname is cached: inside a container it is the container id, which is what
// lets a harness attribute a reached marker to the process that wrote it when
// several replicas announce the same point.
var hostname = func() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}()

// Name identifies the point in logs and in the harness contract.
func (p *Point) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

// Reached reports whether the component announced this point and is still
// waiting for its release. Nil-safe (always false).
func (p *Point) Reached() bool {
	if p == nil {
		return false
	}
	_, err := os.Stat(p.reachedPath())
	return err == nil
}

// Armed reports whether the harness has selected this point for injection.
// Nil-safe (always false). An unarmed point never blocks: the component walks
// through every state it was not asked to stop at, so a scenario pays only for
// the fault it actually injects.
func (p *Point) Armed() bool {
	if p == nil {
		return false
	}
	_, err := os.Stat(p.armedPath())
	return err == nil
}

func (p *Point) armedPath() string {
	return filepath.Join(p.barrier.dir, p.name+armedSuffix)
}

func (p *Point) reachedPath() string {
	return filepath.Join(p.barrier.dir, p.name+reachedSuffix)
}

func (p *Point) releasePath() string {
	return filepath.Join(p.barrier.dir, p.name+releaseSuffix)
}

// Arrive announces that the component reached this point and waits for the
// harness to release it. It is a no-op when no barrier is configured, and also
// when this point is not armed — a scenario only stops where it asked to.
//
// The reached marker is published BEFORE any waiting, because it is the only
// state the harness acts on. The marker is withdrawn when Arrive returns — on
// the release path AND on every failure path — so Reached() is a truthful
// "a component is waiting here right now" predicate rather than a latch that a
// killed process could leave behind. A released point also consumes its release
// marker, so one point serves several scenarios in a run.
func (p *Point) Arrive(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if !p.Armed() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	marker := fmt.Sprintf("%s %d %s\n", hostname, os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(p.reachedPath(), []byte(marker), 0o644); err != nil {
		return fmt.Errorf("fault barrier %s: cannot publish the reached marker: %w", p.name, err)
	}
	defer func() { _ = os.Remove(p.reachedPath()) }()
	if p.barrier.log != nil {
		p.barrier.log.Info("fault barrier reached", zap.String("point", p.name), zap.String("dir", p.barrier.dir))
	}

	deadline := time.Now().Add(p.barrier.timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		switch _, err := os.Stat(p.releasePath()); {
		case err == nil:
			if rmErr := os.Remove(p.releasePath()); rmErr != nil && !os.IsNotExist(rmErr) {
				return fmt.Errorf("fault barrier %s: cannot consume the release marker: %w", p.name, rmErr)
			}
			if p.barrier.log != nil {
				p.barrier.log.Info("fault barrier released", zap.String("point", p.name))
			}
			return nil
		case !os.IsNotExist(err):
			return fmt.Errorf("fault barrier %s: cannot read the release marker: %w", p.name, err)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("fault barrier %s: the component was cancelled while waiting for a release: %w", p.name, err)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("fault barrier %s: no release within %s", p.name, p.barrier.timeout)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("fault barrier %s: the component was cancelled while waiting for a release: %w", p.name, ctx.Err())
		}
	}
}

// Arm selects one point for injection. Any stale reached/release pair left by
// an earlier scenario is cleared first, so the state the harness observes next
// is the state THIS scenario produced and not a leftover.
func (b *Barrier) Arm(name string) error {
	if b == nil {
		return errors.New("fault barrier: no barrier is configured")
	}
	p := b.Point(name)
	for _, path := range []string{p.reachedPath(), p.releasePath()} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("fault barrier %s: cannot clear %s: %w", name, path, err)
		}
	}
	if err := os.WriteFile(p.armedPath(), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("fault barrier %s: cannot arm: %w", name, err)
	}
	return nil
}

// Disarm removes the selection so the point stops blocking.
func (b *Barrier) Disarm(name string) error {
	if b == nil {
		return nil
	}
	if err := os.Remove(b.Point(name).armedPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("fault barrier %s: cannot disarm: %w", name, err)
	}
	return nil
}

// Reset removes every marker in the rendezvous directory. A harness calls it
// before a scenario so that a marker left by a killed process cannot be mistaken
// for the state this scenario produced.
func (b *Barrier) Reset() error {
	if b == nil {
		return nil
	}
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("fault barrier: cannot list %s: %w", b.dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, reachedSuffix) && !strings.HasSuffix(name, releaseSuffix) && !strings.HasSuffix(name, armedSuffix) {
			continue
		}
		if rmErr := os.Remove(filepath.Join(b.dir, name)); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("fault barrier: cannot remove %s: %w", name, rmErr)
		}
	}
	return nil
}

// WaitReached blocks until the component announces the point, so a harness can
// observe the state before it injects the fault. Nil-safe (returns an error).
func (p *Point) WaitReached(ctx context.Context, timeout time.Duration) error {
	if p == nil {
		return errors.New("fault barrier: no barrier is configured for this point")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if p.Reached() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("fault barrier %s: not reached before the context ended: %w", p.name, err)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("fault barrier %s: not reached within %s", p.name, timeout)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("fault barrier %s: not reached before the context ended: %w", p.name, ctx.Err())
		}
	}
}

// Release lets a component that is waiting at this point continue.
func (p *Point) Release() error {
	if p == nil {
		return errors.New("fault barrier: no barrier is configured for this point")
	}
	if err := os.WriteFile(p.releasePath(), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("fault barrier %s: cannot write the release marker: %w", p.name, err)
	}
	return nil
}
