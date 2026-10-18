package signature

import (
	"context"
	"crypto/ecdsa"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fuzzSignerKeyHex is the canonical Anvil account #0 key. It is a public test
// constant already used by this repository's Web3 fixtures, not a real secret.
const fuzzSignerKeyHex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

var fuzzSignerKey = fuzzKeyFromHex(fuzzSignerKeyHex)

func fuzzKeyFromHex(hexKey string) *ecdsa.PrivateKey {
	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		panic("signature fuzz fixture key is invalid: " + err.Error())
	}
	return key
}

// FuzzWalletMessage pins what a wallet signature actually authorizes: the exact
// bytes of one message, and nothing else. A signature must verify against its
// signer for the message it was made over, must never verify for a different
// message (no length-prefix aliasing, so a signature cannot be replayed onto a
// message the wallet never saw), and must never accept an unrelated signature.
func FuzzWalletMessage(f *testing.F) {
	f.Add("hello", "hello!", "")
	f.Add("", "a", "")
	f.Add("a", "aa", "")
	f.Add("23", "3", "")
	f.Add(strings.Repeat("x", 300), strings.Repeat("y", 300), "")
	f.Add("login:0xabc", "", "0x"+strings.Repeat("ab", 65))

	f.Fuzz(func(t *testing.T, message, otherMessage, signature string) {
		sv := NewSignatureVerifier(zap.NewNop())
		address := sv.GetAddressFromPrivateKey(fuzzSignerKey)

		sig, err := sv.SignMessage(message, fuzzSignerKey)
		require.NoError(t, err)
		require.Len(t, sig, 2+65*2, "an EIP-191 signature is 65 bytes of hex")

		ok, err := sv.VerifySignature(context.Background(), address, message, sig)
		require.NoError(t, err, "the signature we just produced must not be rejected as malformed")
		require.True(t, ok, "a signature must verify for the message it was made over")

		if otherMessage != message {
			other, err := sv.VerifySignature(context.Background(), address, otherMessage, sig)
			require.NoError(t, err)
			require.False(t, other, "a signature over %q must not authorize %q", message, otherMessage)
		}

		if signature != "" && signature != sig {
			foreign, _ := sv.VerifySignature(context.Background(), address, message, signature)
			require.False(t, foreign, "an unrelated signature must not authorize %q", message)
		}
	})
}
