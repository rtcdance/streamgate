package solana

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSolanaVerifier_VerifySignature_TableDriven(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	solanaPubKey := solana.PublicKeyFromBytes(pubKey)
	address := solanaPubKey.String()

	message := "test message"
	sig := ed25519.Sign(privKey, []byte(message))
	sigB64 := base64.StdEncoding.EncodeToString(sig)
	msgB64 := base64.StdEncoding.EncodeToString([]byte(message))

	tests := []struct {
		name      string
		address   string
		message   string
		signature string
		expectOk  bool
		expectErr bool
	}{
		{"valid", address, msgB64, sigB64, true, false},
		{"wrong message", address, base64.StdEncoding.EncodeToString([]byte("other")), sigB64, false, false},
		{"invalid address", "bad-address!", msgB64, sigB64, false, true},
		{"invalid sig format", address, msgB64, "!!!not-base64!!!", false, true},
		{"wrong sig length", address, msgB64, base64.StdEncoding.EncodeToString([]byte("short")), false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := sv.VerifySignature(tc.address, tc.message, tc.signature)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectOk, ok)
			}
		})
	}
}

func TestSolanaVerifier_VerifyTransaction(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	solanaPubKey := solana.PublicKeyFromBytes(pubKey)
	address := solanaPubKey.String()

	txData := []byte("transaction data")
	sig := ed25519.Sign(privKey, txData)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	t.Run("valid", func(t *testing.T) {
		ok, err := sv.VerifyTransaction(address, txData, sigB64)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("invalid address", func(t *testing.T) {
		_, err := sv.VerifyTransaction("bad!", txData, sigB64)
		assert.Error(t, err)
	})

	t.Run("invalid sig", func(t *testing.T) {
		_, err := sv.VerifyTransaction(address, txData, "!!!")
		assert.Error(t, err)
	})

	t.Run("wrong sig length", func(t *testing.T) {
		_, err := sv.VerifyTransaction(address, txData, base64.StdEncoding.EncodeToString([]byte("short")))
		assert.Error(t, err)
	})

	t.Run("wrong data", func(t *testing.T) {
		ok, err := sv.VerifyTransaction(address, []byte("wrong"), sigB64)
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestSolanaVerifier_SignMessage(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	_, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	sig, err := sv.SignMessage("hello", privKey)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)

	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	require.NoError(t, err)
	assert.Len(t, sigBytes, 64)
}

func TestSolanaVerifier_SignTransaction(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	_, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	sig, err := sv.SignTransaction([]byte("tx data"), privKey)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)
}

func TestSolanaVerifier_GetPublicKeyFromPrivateKey(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	expectedPubKey := solana.PublicKeyFromBytes(pubKey)
	result := sv.GetPublicKeyFromPrivateKey(privKey)
	assert.Equal(t, expectedPubKey.String(), result)
}

func TestSolanaVerifier_VerifyMessage(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	solanaPubKey := solana.PublicKeyFromBytes(pubKey)
	address := solanaPubKey.String()

	message := base64.StdEncoding.EncodeToString([]byte("hello"))
	sig := ed25519.Sign(privKey, []byte("hello"))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	ok, err := sv.VerifyMessage(address, message, sigB64)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestSolanaVerifier_VerifyMultiSignature(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	pubKey1, privKey1, _ := ed25519.GenerateKey(nil)
	pubKey2, privKey2, _ := ed25519.GenerateKey(nil)

	addr1 := solana.PublicKeyFromBytes(pubKey1).String()
	addr2 := solana.PublicKeyFromBytes(pubKey2).String()

	msg := base64.StdEncoding.EncodeToString([]byte("multi sig"))
	sig1 := base64.StdEncoding.EncodeToString(ed25519.Sign(privKey1, []byte("multi sig")))
	sig2 := base64.StdEncoding.EncodeToString(ed25519.Sign(privKey2, []byte("multi sig")))

	t.Run("all valid", func(t *testing.T) {
		ok, err := sv.VerifyMultiSignature([]string{addr1, addr2}, msg, []string{sig1, sig2})
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("mismatched count", func(t *testing.T) {
		_, err := sv.VerifyMultiSignature([]string{addr1}, msg, []string{sig1, sig2})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})

	t.Run("one invalid", func(t *testing.T) {
		wrongSig := base64.StdEncoding.EncodeToString(ed25519.Sign(privKey2, []byte("wrong")))
		ok, err := sv.VerifyMultiSignature([]string{addr1, addr2}, msg, []string{wrongSig, sig2})
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestSolanaVerifier_VerifyOffchainMessage(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	solanaPubKey := solana.PublicKeyFromBytes(pubKey)
	address := solanaPubKey.String()

	message := "offchain test"
	encoded := encodeSIP004Message(message)
	sig := ed25519.Sign(privKey, encoded)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	ok, err := sv.VerifyOffchainMessage(address, message, sigB64)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestSolanaVerifier_SignOffchainMessage(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	_, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	sig, err := sv.SignOffchainMessage("offchain msg", privKey)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)
}

func TestSolanaVerifier_ParseSolanaAddress(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	t.Run("base58 address", func(t *testing.T) {
		pubKey, _, _ := ed25519.GenerateKey(nil)
		solanaPubKey := solana.PublicKeyFromBytes(pubKey)
		addr := solanaPubKey.String()

		parsed, err := sv.ParseSolanaAddress(addr)
		require.NoError(t, err)
		assert.Equal(t, solanaPubKey, parsed)
	})

	t.Run("0x hex address", func(t *testing.T) {
		pubKey, _, _ := ed25519.GenerateKey(nil)
		hexAddr := "0x" + hex.EncodeToString(pubKey)

		parsed, err := sv.ParseSolanaAddress(hexAddr)
		require.NoError(t, err)
		assert.Equal(t, solana.PublicKeyFromBytes(pubKey), parsed)
	})

	t.Run("invalid base58", func(t *testing.T) {
		_, err := sv.ParseSolanaAddress("invalid!@#")
		require.Error(t, err)
	})

	t.Run("invalid hex length", func(t *testing.T) {
		_, err := sv.ParseSolanaAddress("0x1234")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid address length")
	})

	t.Run("invalid hex", func(t *testing.T) {
		_, err := sv.ParseSolanaAddress("0xZZZZ")
		require.Error(t, err)
	})
}

func TestSolanaVerifier_IsValidAddress_TableDriven(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	tests := []struct {
		name    string
		address string
		valid   bool
	}{
		{"valid base58", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", true},
		{"invalid", "not-a-valid-address!", false},
		{"empty", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.valid, sv.IsValidAddress(tc.address))
		})
	}
}

func TestSolanaVerifier_VerifyPDASignature(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "")

	t.Run("valid PDA", func(t *testing.T) {
		programID := "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
		seeds := []string{"test-seed"}
		programPubKey := solana.MustPublicKeyFromBase58(programID)
		expectedPDA, _, _ := solana.FindProgramAddress([][]byte{[]byte("test-seed")}, programPubKey)

		ok, err := sv.VerifyPDASignature(expectedPDA.String(), seeds, programID)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("invalid PDA", func(t *testing.T) {
		ok, err := sv.VerifyPDASignature("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", []string{"test"}, "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("invalid program ID", func(t *testing.T) {
		_, err := sv.VerifyPDASignature("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", []string{"test"}, "invalid!")
		require.Error(t, err)
	})

	t.Run("invalid PDA address", func(t *testing.T) {
		_, err := sv.VerifyPDASignature("invalid!", []string{"test"}, "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
		require.Error(t, err)
	})
}

func TestSolanaVerifier_WithRPCClient_NoClients(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop())
	err := sv.withRPCClient(func(client *rpc.Client) error {
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestSolanaVerifier_Close(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://127.0.0.1:1")
	assert.NotPanics(t, func() {
		sv.Close()
	})
}

func TestSolanaVerifier_SwitchToNextRPC(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://a", "http://b")
	sv.switchToNextRPC()
	assert.Equal(t, uint32(1), sv.currentIdx.Load()%2)
}

func TestSolanaVerifier_SwitchToNextRPC_SingleClient(t *testing.T) {
	sv := NewSolanaVerifier(zap.NewNop(), "http://a")
	sv.switchToNextRPC()
}

func TestEncodeSIP004Message(t *testing.T) {
	msg := encodeSIP004Message("hello")
	assert.Equal(t, byte(0xff), msg[0])
	assert.Contains(t, string(msg[1:16]), "solana offchain")
}

func TestEncodeVarint(t *testing.T) {
	tests := []struct {
		input  uint64
		expect int
	}{
		{0, 1},
		{127, 1},
		{128, 2},
		{16383, 2},
		{16384, 3},
	}

	for _, tc := range tests {
		result := encodeVarint(tc.input)
		assert.Equal(t, tc.expect, len(result))
	}
}
