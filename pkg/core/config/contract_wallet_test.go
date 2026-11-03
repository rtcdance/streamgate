package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestContractWalletPolicy_DefaultsAndCeilings(t *testing.T) {
	d := DefaultContractWalletConfig()
	require.Equal(t, d, (ContractWalletConfig{}).Effective())
	require.Equal(t, d, DefaultConfig().Auth.ContractWallet)
	require.NoError(t, d.Validate())
	tests := []struct {
		name   string
		change func(*ContractWalletConfig)
	}{
		{"lease_ttl", func(p *ContractWalletConfig) { p.LeaseTTL = time.Minute + time.Nanosecond }},
		{"proof_max_age", func(p *ContractWalletConfig) { p.ProofMaxAge = 6 * time.Second }},
		{"clock_skew", func(p *ContractWalletConfig) { p.ClockSkew = 6 * time.Second }},
		{"absolute_session_ttl", func(p *ContractWalletConfig) { p.AbsoluteSessionTTL = 3 * time.Hour }},
		{"max_signature_bytes", func(p *ContractWalletConfig) { p.MaxSignatureBytes = 8193 }},
		{"max_body_bytes", func(p *ContractWalletConfig) { p.MaxBodyBytes = 20*1024 + 1 }},
		{"call_gas_limit", func(p *ContractWalletConfig) { p.CallGasLimit = 500001 }},
		{"verification_timeout", func(p *ContractWalletConfig) { p.VerificationTimeout = 6 * time.Second }},
		{"negative gas", func(p *ContractWalletConfig) { p.CallGasLimit = -1 }},
		{"negative time", func(p *ContractWalletConfig) { p.ProofMaxAge = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := d
			tt.change(&p)
			require.Error(t, p.Validate())
			cfg := DefaultConfig()
			cfg.Auth.JWTSecret = "contract-policy-validation-secret-at-least-32"
			cfg.Auth.ContractWallet = p
			require.ErrorContains(t, cfg.ValidateProduction(zap.NewNop()), "auth.contract_wallet")
		})
	}
	strict := ContractWalletConfig{LeaseTTL: 20 * time.Second, ProofMaxAge: time.Second, ClockSkew: time.Second, AbsoluteSessionTTL: 30 * time.Minute,
		MaxSignatureBytes: 1024, MaxBodyBytes: 4096, CallGasLimit: 200000, VerificationTimeout: time.Second}
	require.NoError(t, strict.Validate())
}

func TestContractWalletPolicy_LoadRejectsMalformedAndOversizedInputs(t *testing.T) {
	for _, tt := range []struct{ key, value string }{
		{"LEASE_TTL", "61s"}, {"LEASE_TTL", "garbage"}, {"CALL_GAS_LIMIT", "-1"}, {"MAX_SIGNATURE_BYTES", "not-an-integer"},
	} {
		t.Run(tt.key+tt.value, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			t.Setenv("STREAMGATE_AUTH_CONTRACT_WALLET_"+tt.key, tt.value)
			_, err := LoadConfig()
			require.ErrorContains(t, err, "auth.contract_wallet")
		})
	}
	viper.Reset()
	defer viper.Reset()
	t.Setenv("STREAMGATE_AUTH_CONTRACT_WALLET_LEASE_TTL", "20s")
	t.Setenv("STREAMGATE_AUTH_CONTRACT_WALLET_CALL_GAS_LIMIT", "100000")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, 20*time.Second, cfg.Auth.ContractWallet.LeaseTTL)
	require.Equal(t, int64(100000), cfg.Auth.ContractWallet.CallGasLimit)
}
