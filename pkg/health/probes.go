package health

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
)

// CheckSpec is a named dependency check the readiness probe evaluates.
type CheckSpec struct {
	Name  string
	Check HealthCheck
}

// NewCheckerWithChecks builds a checker with the given dependency checks
// pre-registered, so a caller composing readiness for a profile does not have
// to register each one by hand.
func NewCheckerWithChecks(logger *zap.Logger, checks []CheckSpec) *HealthChecker {
	hc := NewHealthChecker(logger)
	for _, spec := range checks {
		hc.RegisterCheck(spec.Name, spec.Check)
	}
	return hc
}

// ChainIDFunc fetches the chain id reported by one RPC endpoint.
type ChainIDFunc func(ctx context.Context, endpoint string) (int64, error)

// RPCCheck returns a check that is healthy when AT LEAST ONE configured
// endpoint answers eth_chainId with the expected chain. A single bad endpoint
// with a working fallback stays ready (no false 503); every endpoint bad is
// not ready. Endpoints are tried in order and the first healthy one wins.
func RPCCheck(endpoints []string, wantChainID int64, fetch ChainIDFunc) HealthCheck {
	return func(ctx context.Context) error {
		if wantChainID <= 0 {
			return errors.New("no chain id configured")
		}
		var lastErr error
		tried := 0
		for _, ep := range endpoints {
			if ep == "" {
				continue
			}
			tried++
			got, err := fetch(ctx, ep)
			if err != nil {
				lastErr = fmt.Errorf("%s: %w", ep, err)
				continue
			}
			if got != wantChainID {
				lastErr = fmt.Errorf("%s: chain id %d does not match configured %d", ep, got, wantChainID)
				continue
			}
			return nil
		}
		if tried == 0 {
			return errors.New("no RPC endpoints configured")
		}
		return fmt.Errorf("no healthy same-chain RPC endpoint (%d tried): %w", tried, lastErr)
	}
}

// PingCheck adapts a context-aware ping to a HealthCheck under a name.
func PingCheck(name string, ping func(ctx context.Context) error) HealthCheck {
	return func(ctx context.Context) error {
		if err := ping(ctx); err != nil {
			return fmt.Errorf("%s check failed: %w", name, err)
		}
		return nil
	}
}

// ConnectCheck adapts a zero-arg connect (e.g. NATS) to a HealthCheck.
func ConnectCheck(name string, connect func() error) HealthCheck {
	return func(ctx context.Context) error {
		if err := connect(); err != nil {
			return fmt.Errorf("%s check failed: %w", name, err)
		}
		return nil
	}
}
