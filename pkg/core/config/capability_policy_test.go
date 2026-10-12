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

// The transfer-revocation bound is the SUM of the three windows, not an
// independent constant. Pinning the arithmetic means widening any one component
// fails here rather than the guarantee silently loosening.
func TestCapabilityPolicy_TransferBoundIsTheSumOfItsParts(t *testing.T) {
	assert.Equal(t,
		MaxTransferRevocationBound,
		DefaultCapabilityTTL+DefaultPositiveCacheTTL+DefaultClockSkew,
		"the 70s bound must equal capability TTL + positive cache + clock skew")
	assert.Equal(t, 70*time.Second, MaxTransferRevocationBound)
}

// The per-field maxima are what make the sum hold today, so they must not drift
// apart from it either.
func TestCapabilityPolicy_MaximaSumToTheBound(t *testing.T) {
	assert.LessOrEqual(t,
		MaxCapabilityTTL+MaxPositiveCacheTTL+MaxClockSkew,
		MaxTransferRevocationBound,
		"individually-permitted maxima must not add up past the transfer bound")
}

// A config that is over the bound is a startup failure. This also exercises the
// sum check directly, which the per-field maxima would otherwise mask.
func TestValidateProduction_RejectsASumOverTheBound(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.JWTSecret = "0123456789-0123456789-0123456789-x"
	// Each value is at or under its own ceiling; the total is what breaks.
	cfg.Features.CapabilityTTL = MaxCapabilityTTL
	cfg.Features.PositiveCacheTTL = MaxPositiveCacheTTL
	cfg.Features.ClockSkew = MaxClockSkew + time.Second

	err := cfg.ValidateProduction(zap.NewNop())
	assert.Error(t, err)
	if err != nil {
		assert.Contains(t, err.Error(), "clock_skew")
		assert.Contains(t, err.Error(), "transfer-revocation bound")
	}
}

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
