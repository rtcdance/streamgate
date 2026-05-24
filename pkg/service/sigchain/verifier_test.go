package sigchain

import (
	"context"
	"errors"
	"testing"

	"github.com/rtcdance/streamgate/pkg/service/serviceerrors"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockEVMVerifier struct {
	result bool
	err    error
}

func (m *mockEVMVerifier) VerifySignature(_ context.Context, _, _, _ string) (bool, error) {
	return m.result, m.err
}

type mockSolanaSigner struct {
	sigResult      bool
	sigErr         error
	offchainResult bool
	offchainErr    error
}

func (m *mockSolanaSigner) VerifySignature(_, _, _ string) (bool, error) {
	return m.sigResult, m.sigErr
}

func (m *mockSolanaSigner) VerifyOffchainMessage(_, _, _ string) (bool, error) {
	return m.offchainResult, m.offchainErr
}

func TestNewMultiChainSignatureVerifier(t *testing.T) {
	t.Run("with solana signer", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), &mockSolanaSigner{})
		require.NotNil(t, v)
		assert.NotNil(t, v.evmVerifier)
		assert.NotNil(t, v.solanaVerifier)
	})

	t.Run("without solana signer", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), nil)
		require.NotNil(t, v)
		assert.NotNil(t, v.evmVerifier)
		assert.Nil(t, v.solanaVerifier)
	})
}

func TestMultiChainSignatureVerifier_VerifySignature(t *testing.T) {
	t.Run("delegates to evm verifier", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), nil)
		result, err := v.VerifySignature(context.Background(), "0xInvalidAddress", "message", "0xsig")
		_ = result
		_ = err
	})
}

func TestMultiChainSignatureVerifier_VerifySolanaSignature(t *testing.T) {
	t.Run("no solana verifier configured", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), nil)
		result, err := v.VerifySolanaSignature(context.Background(), "addr", "msg", "sig")
		assert.False(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, serviceerrors.ErrSolanaNotConfigured))
	})

	t.Run("solana verifier returns true", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), &mockSolanaSigner{sigResult: true})
		result, err := v.VerifySolanaSignature(context.Background(), "addr", "msg", "sig")
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("solana verifier returns false", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), &mockSolanaSigner{sigResult: false})
		result, err := v.VerifySolanaSignature(context.Background(), "addr", "msg", "sig")
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("solana verifier returns error", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), &mockSolanaSigner{sigErr: errors.New("verification failed")})
		result, err := v.VerifySolanaSignature(context.Background(), "addr", "msg", "sig")
		assert.False(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "verification failed")
	})
}

func TestMultiChainSignatureVerifier_VerifyOffchainMessage(t *testing.T) {
	t.Run("no solana verifier configured", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), nil)
		result, err := v.VerifyOffchainMessage(context.Background(), "addr", "msg", "sig")
		assert.False(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, serviceerrors.ErrSolanaNotConfigured))
	})

	t.Run("solana verifier returns true", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), &mockSolanaSigner{offchainResult: true})
		result, err := v.VerifyOffchainMessage(context.Background(), "addr", "msg", "sig")
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("solana verifier returns false", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), &mockSolanaSigner{offchainResult: false})
		result, err := v.VerifyOffchainMessage(context.Background(), "addr", "msg", "sig")
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("solana verifier returns error", func(t *testing.T) {
		v := NewMultiChainSignatureVerifier(zap.NewNop(), &mockSolanaSigner{offchainErr: errors.New("offchain failed")})
		result, err := v.VerifyOffchainMessage(context.Background(), "addr", "msg", "sig")
		assert.False(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "offchain failed")
	})
}

func TestMultiChainSignatureVerifier_InterfaceCompliance(t *testing.T) {
	var _ web3.SolanaSigner = &mockSolanaSigner{}
}
