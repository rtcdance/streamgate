package gateway

import (
	"context"
	"sync"
	"testing"

	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeVersionStore stands in for the rule-version authority and lets a test move
// the version without going through the local adapter.
type fakeVersionStore struct {
	mu      sync.Mutex
	version int64
	err     error
	reads   int
}

func (f *fakeVersionStore) Read(_ context.Context, _ string) (storage.RuleVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return storage.RuleVersion{}, f.err
	}
	return storage.RuleVersion{Version: f.version}, nil
}

func (f *fakeVersionStore) Bump(_ context.Context, _ string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version++
	return f.version, nil
}

func (f *fakeVersionStore) setVersion(v int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version = v
}

func (f *fakeVersionStore) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// A cached rule set is reusable only while the authority still reports the same
// version. This is what makes a write in ANOTHER process visible: the local
// flush only ever covers this process's own writes.
func TestVersionedGatingRuleResolver_ReReadsWhenTheVersionMoves(t *testing.T) {
	ctx := context.Background()
	readCount := 0
	db := &gatingMockDB{
		queryFn: func(context.Context, string, ...interface{}) (storage.Rows, error) {
			readCount++
			return &gatingRuleRowScanner{max: 1}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	versions := &fakeVersionStore{version: 1}
	adapter := NewVersionedGatingRuleResolverAdapter(svc, versions)

	rules, err := adapter.GetActiveRulesForContent(ctx, "c1")
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, 1, readCount)

	// Same version: the cached set is reused and the rules source is untouched.
	_, err = adapter.GetActiveRulesForContent(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, 1, readCount, "an unchanged version must reuse the cached rule set")

	// A version move that did NOT come through this adapter - i.e. a write in
	// another process - must force a re-read.
	versions.setVersion(2)
	_, err = adapter.GetActiveRulesForContent(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, 2, readCount, "a version change must invalidate the cached rule set")
}

// An unreadable authority is a refusal. Serving the cached rules anyway would
// extend the very window the version exists to bound.
func TestVersionedGatingRuleResolver_UnreadableAuthorityRefuses(t *testing.T) {
	ctx := context.Background()
	db := &gatingMockDB{
		queryFn: func(context.Context, string, ...interface{}) (storage.Rows, error) {
			return &gatingRuleRowScanner{max: 1}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	versions := &fakeVersionStore{version: 1}
	adapter := NewVersionedGatingRuleResolverAdapter(svc, versions)

	_, err := adapter.GetActiveRulesForContent(ctx, "c1")
	require.NoError(t, err)

	versions.setErr(storage.ErrRuleVersionUnavailable)
	_, err = adapter.GetActiveRulesForContent(ctx, "c1")
	assert.ErrorIs(t, err, storage.ErrRuleVersionUnavailable,
		"rules must not be served when the version authority cannot be read")
}

// Without a version store the adapter keeps its previous TTL-only behaviour, so
// the older constructor still works.
func TestGatingRuleResolverAdapter_WithoutVersionsUsesTheTTL(t *testing.T) {
	ctx := context.Background()
	readCount := 0
	db := &gatingMockDB{
		queryFn: func(context.Context, string, ...interface{}) (storage.Rows, error) {
			readCount++
			return &gatingRuleRowScanner{max: 1}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	adapter := NewGatingRuleResolverAdapter(svc)

	_, err := adapter.GetActiveRulesForContent(ctx, "c1")
	require.NoError(t, err)
	_, err = adapter.GetActiveRulesForContent(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, 1, readCount, "the TTL path must still serve from cache")
}
