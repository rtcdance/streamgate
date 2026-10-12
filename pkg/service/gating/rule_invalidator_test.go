package gating

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Rule writes notify a registered invalidator so caches holding the previous
// rule set cannot keep authorising against it.
func TestGatingRuleService_InvalidatorHook(t *testing.T) {
	svc := &GatingRuleService{}

	var got []string
	svc.SetInvalidator(func(contentID string) { got = append(got, contentID) })

	svc.notifyInvalidate("c1")
	svc.notifyInvalidate("") // a delete drops everything

	assert.Equal(t, []string{"c1", ""}, got)
}

// A service with no invalidator registered must not panic: it is optional
// wiring, and the resolver may not be present in every deployment.
func TestGatingRuleService_NoInvalidatorIsSafe(t *testing.T) {
	svc := &GatingRuleService{}
	assert.NotPanics(t, func() { svc.notifyInvalidate("c1") })
}

// Re-registering replaces rather than appends, so rewiring cannot fan out to a
// stale resolver.
func TestGatingRuleService_InvalidatorReplacement(t *testing.T) {
	svc := &GatingRuleService{}

	first, second := 0, 0
	svc.SetInvalidator(func(string) { first++ })
	svc.SetInvalidator(func(string) { second++ })

	svc.notifyInvalidate("c1")

	assert.Equal(t, 0, first, "the replaced invalidator must not be called")
	assert.Equal(t, 1, second)
}
