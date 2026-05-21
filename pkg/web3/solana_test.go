package web3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMultiChainManager_SolanaChains(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	t.Run("add Solana chain with negative ID", func(t *testing.T) {
		err := mcm.AddChain(-1)
		require.NoError(t, err)

		client, err := mcm.GetSolanaClient(-1)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("add Solana Devnet", func(t *testing.T) {
		err := mcm.AddChain(-2)
		require.NoError(t, err)

		client, err := mcm.GetSolanaClient(-2)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("EVM client not found for Solana chain ID", func(t *testing.T) {
		_, err := mcm.GetClient(-1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "EVM chain client not found")
	})

	t.Run("Solana client not found for EVM chain ID", func(t *testing.T) {
		_, err := mcm.GetSolanaClient(1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Solana chain client not found")
	})

	t.Run("unsupported chain ID", func(t *testing.T) {
		err := mcm.AddChain(-999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chain not supported")
	})

	t.Run("remove Solana chain", func(t *testing.T) {
		mcm.RemoveChain(-2)
		_, err := mcm.GetSolanaClient(-2)
		assert.Error(t, err)
	})

	mcm.Close()
}
