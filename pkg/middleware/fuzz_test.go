package middleware

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// FuzzNFTQuantity pins the gate's authorization rule: access is granted if and
// only if the quantity the verifier actually reports reaches the rule's
// (normalized) min_balance. It also pins the two ways the gate must never be
// talked into granting: an unusable verifier answer — an error, a nil, or a
// negative quantity — and a malformed rule (a negative min_balance).
func FuzzNFTQuantity(f *testing.F) {
	f.Add(int64(3), 2, uint8(0))   // 3 >= 2  → grant
	f.Add(int64(0), 1, uint8(0))   // 0 <  1  → deny
	f.Add(int64(0), 0, uint8(0))   // min_balance 0 normalizes to 1 → deny at 0
	f.Add(int64(1), 0, uint8(0))   // ...and grant at 1
	f.Add(int64(10), 10, uint8(0)) // at the boundary → grant
	f.Add(int64(9), 10, uint8(0))  // just below the boundary → deny
	f.Add(int64(5), -1, uint8(0))  // malformed rule
	f.Add(int64(5), 1, uint8(1))   // verifier error
	f.Add(int64(5), 1, uint8(2))   // verifier returns nil
	f.Add(int64(-7), 1, uint8(0))  // verifier returns a negative quantity

	f.Fuzz(func(t *testing.T, observed int64, minBalance int, mode uint8) {
		rule := GatingRule{
			ContractAddress: testContractAddr,
			ChainID:         1,
			Standard:        "erc721",
			MinBalance:      minBalance,
		}
		verifier := &mockNFTOwnershipCheckerOld{
			quantityFn: func(context.Context, int64, string, string, string) (*big.Int, error) {
				switch mode % 3 {
				case 1:
					return nil, errors.New("chain read failed")
				case 2:
					// A nil quantity is not evidence of ownership.
					return nil, nil
				default:
					return big.NewInt(observed), nil
				}
			},
		}
		config := &NFTGateConfig{
			Verifier:       verifier,
			BlockProver:    &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xanchor"}},
			DefaultChainID: 1,
		}

		decision, err := EvaluatePolicy(context.Background(), config, zap.NewNop(), []GatingRule{rule}, "0xOwner")

		// A malformed rule is rejected outright and can never grant.
		if minBalance < 0 {
			require.Error(t, err, "a negative min_balance must be rejected")
			assert.False(t, decision.Allowed)
			assert.Nil(t, decision.GrantingRule)
			return
		}
		// A verifier answer that carries no usable quantity can never grant.
		if mode%3 != 0 || observed < 0 {
			assert.False(t, decision.Allowed, "no usable quantity was reported, so access must not be granted")
			assert.Nil(t, decision.GrantingRule)
			return
		}

		// Granted exactly when the observed quantity reached the effective minimum.
		effective := minBalance
		if effective == 0 {
			effective = 1
		}
		want := observed >= int64(effective)
		require.NoError(t, err)
		assert.Equal(t, want, decision.Allowed,
			"quantity %d against effective min_balance %d", observed, effective)
		if want {
			require.NotNil(t, decision.GrantingRule, "a grant must name the rule that granted it")
			assert.NotNil(t, decision.Anchor, "a grant must carry the block it was read at")
		} else {
			assert.Nil(t, decision.GrantingRule)
		}
	})
}
