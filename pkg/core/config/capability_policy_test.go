package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// Each of these bounds widens the window in which a revoked, transferred or
// rule-blocked holding still plays. A production deployment that exceeds one
// must fail to start: the excess is invisible from outside.
func TestValidateProduction_RejectsOversizedCapabilityWindows(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.JWTSecret = "0123456789-0123456789-0123456789-x"
	cfg.Features.CapabilityTTL = 30 * time.Minute
	cfg.Features.PositiveCacheTTL = time.Minute
	cfg.Features.ClockSkew = time.Minute

	err := cfg.ValidateProduction(zap.NewNop())
	assert.Error(t, err, "oversized capability windows must be a startup failure")

	msg := ""
	if err != nil {
		msg = err.Error()
	}
	assert.Contains(t, msg, "capability_ttl")
	assert.Contains(t, msg, "positive_cache_ttl")
	assert.Contains(t, msg, "clock_skew")
}

// The shipped defaults are the policy, so they must never be what fails.
func TestValidateProduction_AcceptsPolicyDefaults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.JWTSecret = "0123456789-0123456789-0123456789-x"

	assert.Equal(t, DefaultCapabilityTTL, cfg.Features.CapabilityTTL)
	assert.Equal(t, DefaultPositiveCacheTTL, cfg.Features.PositiveCacheTTL)
	assert.Equal(t, DefaultClockSkew, cfg.Features.ClockSkew)

	if err := cfg.ValidateProduction(zap.NewNop()); err != nil {
		msg := err.Error()
		assert.NotContains(t, msg, "capability_ttl", "the default must satisfy the policy")
		assert.NotContains(t, msg, "positive_cache_ttl", "the default must satisfy the policy")
		assert.NotContains(t, msg, "clock_skew", "the default must satisfy the policy")
	}
}

// Exactly at the ceiling is allowed: the bound is a maximum, not an exclusive
// limit, and rejecting the documented value would make the default unusable.
func TestValidateProduction_AllowsExactlyTheCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.JWTSecret = "0123456789-0123456789-0123456789-x"
	cfg.Features.CapabilityTTL = MaxCapabilityTTL
	cfg.Features.PositiveCacheTTL = MaxPositiveCacheTTL
	cfg.Features.ClockSkew = MaxClockSkew

	if err := cfg.ValidateProduction(zap.NewNop()); err != nil {
		msg := err.Error()
		assert.NotContains(t, msg, "capability_ttl")
		assert.NotContains(t, msg, "positive_cache_ttl")
		assert.NotContains(t, msg, "clock_skew")
	}
}
