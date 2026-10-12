package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A rule write must be visible immediately. Relying on the 30s TTL alone let a
// retargeted rule keep authorising against the old contract for the whole
// window, which is how a tampered rule stayed effective.
func TestGatingRuleResolverAdapter_Invalidate(t *testing.T) {
	fresh := func() map[string]cachedRules {
		return map[string]cachedRules{
			"c1": {expiresAt: time.Now().Add(time.Minute)},
			"c2": {expiresAt: time.Now().Add(time.Minute)},
		}
	}

	t.Run("one content", func(t *testing.T) {
		a := &gatingRuleResolverAdapter{cache: fresh()}
		a.Invalidate("c1")
		assert.NotContains(t, a.cache, "c1")
		assert.Contains(t, a.cache, "c2", "an unrelated content must keep its cache")
	})

	t.Run("empty content drops everything", func(t *testing.T) {
		a := &gatingRuleResolverAdapter{cache: fresh()}
		a.Invalidate("")
		assert.Empty(t, a.cache, "a delete no longer knows the content, so drop all")
	})
}

// Constructing the adapter on a nil service must not panic: the wiring tolerates
// a deployment without a gating-rule service.
func TestNewGatingRuleResolverAdapter_NilService(t *testing.T) {
	assert.NotPanics(t, func() {
		a := NewGatingRuleResolverAdapter(nil)
		assert.NotNil(t, a)
	})
}
