package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// ContractWalletConfig may tighten these budgets, never widen the protocol's
// revocation/resource ceilings. Zero fields use defaults for older configs.
type ContractWalletConfig struct {
	LeaseTTL            time.Duration
	ProofMaxAge         time.Duration
	ClockSkew           time.Duration
	AbsoluteSessionTTL  time.Duration
	MaxSignatureBytes   int
	MaxBodyBytes        int64
	CallGasLimit        int64
	VerificationTimeout time.Duration
}

func DefaultContractWalletConfig() ContractWalletConfig {
	return ContractWalletConfig{LeaseTTL: time.Minute, ProofMaxAge: 5 * time.Second, ClockSkew: 5 * time.Second, AbsoluteSessionTTL: 2 * time.Hour,
		MaxSignatureBytes: 8192, MaxBodyBytes: 20 * 1024, CallGasLimit: 500000, VerificationTimeout: 5 * time.Second}
}

func (p ContractWalletConfig) Effective() ContractWalletConfig {
	d := DefaultContractWalletConfig()
	if p.LeaseTTL == 0 {
		p.LeaseTTL = d.LeaseTTL
	}
	if p.ProofMaxAge == 0 {
		p.ProofMaxAge = d.ProofMaxAge
	}
	if p.ClockSkew == 0 {
		p.ClockSkew = d.ClockSkew
	}
	if p.AbsoluteSessionTTL == 0 {
		p.AbsoluteSessionTTL = d.AbsoluteSessionTTL
	}
	if p.MaxSignatureBytes == 0 {
		p.MaxSignatureBytes = d.MaxSignatureBytes
	}
	if p.MaxBodyBytes == 0 {
		p.MaxBodyBytes = d.MaxBodyBytes
	}
	if p.CallGasLimit == 0 {
		p.CallGasLimit = d.CallGasLimit
	}
	if p.VerificationTimeout == 0 {
		p.VerificationTimeout = d.VerificationTimeout
	}
	return p
}

func (p ContractWalletConfig) Validate() error {
	p = p.Effective()
	d := DefaultContractWalletConfig()
	durations := []struct {
		name           string
		value, ceiling time.Duration
	}{
		{"lease_ttl", p.LeaseTTL, d.LeaseTTL}, {"proof_max_age", p.ProofMaxAge, d.ProofMaxAge}, {"clock_skew", p.ClockSkew, d.ClockSkew},
		{"absolute_session_ttl", p.AbsoluteSessionTTL, d.AbsoluteSessionTTL}, {"verification_timeout", p.VerificationTimeout, d.VerificationTimeout},
	}
	for _, limit := range durations {
		if limit.value < 0 || limit.value > limit.ceiling {
			return fmt.Errorf("auth.contract_wallet.%s must be positive and at most %s", limit.name, limit.ceiling)
		}
	}
	if p.MaxSignatureBytes < 1 || p.MaxSignatureBytes > d.MaxSignatureBytes {
		return fmt.Errorf("auth.contract_wallet.max_signature_bytes exceeds supported budget")
	}
	if p.MaxBodyBytes < 1 || p.MaxBodyBytes > d.MaxBodyBytes {
		return fmt.Errorf("auth.contract_wallet.max_body_bytes exceeds supported budget")
	}
	if p.CallGasLimit < 1 || p.CallGasLimit > d.CallGasLimit {
		return fmt.Errorf("auth.contract_wallet.call_gas_limit exceeds supported budget")
	}
	if p.LeaseTTL+p.ProofMaxAge+p.ClockSkew > 70*time.Second {
		return fmt.Errorf("auth.contract_wallet revocation budget exceeds 70s")
	}
	return nil
}

func validateContractWalletInput() error {
	for _, name := range []string{"lease_ttl", "proof_max_age", "clock_skew", "absolute_session_ttl", "verification_timeout"} {
		key := "auth.contract_wallet." + name
		if _, err := time.ParseDuration(strings.TrimSpace(viper.GetString(key))); err != nil {
			return fmt.Errorf("%s must be a duration", key)
		}
	}
	for _, name := range []string{"max_signature_bytes", "max_body_bytes", "call_gas_limit"} {
		key := "auth.contract_wallet." + name
		if _, err := strconv.ParseInt(strings.TrimSpace(viper.GetString(key)), 10, 64); err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
	}
	return nil
}
