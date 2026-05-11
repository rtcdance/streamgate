package web3

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testPermitTypedData() *EIP712TypedData {
	domain := EIP712Domain{
		Name:              "StreamGate",
		Version:           "1",
		ChainId:           big.NewInt(1),
		VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
	}
	return CreatePermitTypedData(
		domain,
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		big.NewInt(1000),
		big.NewInt(0),
		big.NewInt(1716000000),
	)
}

func TestEIP712Verifier_SignAndVerify(t *testing.T) {
	ev := NewEIP712Verifier(zap.NewNop())

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	typedData := testPermitTypedData()

	signature, err := ev.SignTypedData(typedData, privateKey)
	require.NoError(t, err)

	valid, err := ev.VerifyTypedData(address, typedData, signature)
	require.NoError(t, err)
	assert.True(t, valid, "typed data signature should verify for the correct address")
}

func TestEIP712Verifier_VerifyWrongAddress(t *testing.T) {
	ev := NewEIP712Verifier(zap.NewNop())

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	typedData := testPermitTypedData()

	signature, err := ev.SignTypedData(typedData, privateKey)
	require.NoError(t, err)

	wrongAddress := "0x1111111111111111111111111111111111111111"
	valid, err := ev.VerifyTypedData(wrongAddress, typedData, signature)
	require.NoError(t, err)
	assert.False(t, valid, "typed data signature should NOT verify for a different address")
}

func TestEIP712Verifier_InvalidSignature(t *testing.T) {
	ev := NewEIP712Verifier(zap.NewNop())
	typedData := testPermitTypedData()

	t.Run("too short", func(t *testing.T) {
		_, err := ev.VerifyTypedData("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", typedData, "0x1234")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid signature length")
	})

	t.Run("invalid hex", func(t *testing.T) {
		_, err := ev.VerifyTypedData("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", typedData, "0xzzzz")
		assert.Error(t, err)
	})
}

func TestCreatePermitTypedData(t *testing.T) {
	domain := EIP712Domain{
		Name:              "StreamGate",
		Version:           "1",
		ChainId:           big.NewInt(137),
		VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
	}

	owner := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"
	spender := "0x8ba1f109551bD432803012645Ac136ddd64DBA72"
	value := big.NewInt(500)
	nonce := big.NewInt(1)
	deadline := big.NewInt(1716000000)

	td := CreatePermitTypedData(domain, owner, spender, value, nonce, deadline)

	// Primary type
	assert.Equal(t, "Permit", td.PrimaryType)

	// Domain
	assert.Equal(t, "StreamGate", td.Domain.Name)
	assert.Equal(t, "1", td.Domain.Version)
	assert.Equal(t, big.NewInt(137), td.Domain.ChainId)
	assert.Equal(t, "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC", td.Domain.VerifyingContract)

	// Types: must contain EIP712Domain and Permit
	assert.Contains(t, td.Types, "EIP712Domain")
	assert.Contains(t, td.Types, "Permit")

	// EIP712Domain type fields
	domainTypes := td.Types["EIP712Domain"]
	assert.Equal(t, 4, len(domainTypes))

	// Permit type fields
	permitTypes := td.Types["Permit"]
	assert.Equal(t, 5, len(permitTypes))
	permitFieldNames := make([]string, len(permitTypes))
	for i, f := range permitTypes {
		permitFieldNames[i] = f.Name
	}
	assert.Contains(t, permitFieldNames, "owner")
	assert.Contains(t, permitFieldNames, "spender")
	assert.Contains(t, permitFieldNames, "value")
	assert.Contains(t, permitFieldNames, "nonce")
	assert.Contains(t, permitFieldNames, "deadline")

	// Message values
	assert.Equal(t, owner, td.Message["owner"])
	assert.Equal(t, spender, td.Message["spender"])
	assert.Equal(t, "500", td.Message["value"])
	assert.Equal(t, "1", td.Message["nonce"])
	assert.Equal(t, "1716000000", td.Message["deadline"])
}

// --- Benchmarks ---

func BenchmarkEIP712_EncodeType(b *testing.B) {
	types := []EIP712Type{
		{Name: "owner", Type: "address"},
		{Name: "spender", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "nonce", Type: "uint256"},
		{Name: "deadline", Type: "uint256"},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		EncodeType("Permit", types)
	}
}

func BenchmarkEIP712_TypeHash(b *testing.B) {
	types := []EIP712Type{
		{Name: "owner", Type: "address"},
		{Name: "spender", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "nonce", Type: "uint256"},
		{Name: "deadline", Type: "uint256"},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		TypeHash("Permit", types)
	}
}

func BenchmarkEIP712_HashStruct(b *testing.B) {
	typedData := testPermitTypedData()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = HashStruct(typedData.PrimaryType, typedData.Types, typedData.Message)
	}
}

func BenchmarkEIP712Verifier_SignTypedData(b *testing.B) {
	ev := NewEIP712Verifier(zap.NewNop())
	privateKey, err := crypto.GenerateKey()
	require.NoError(b, err)
	typedData := testPermitTypedData()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ev.SignTypedData(typedData, privateKey)
	}
}

func BenchmarkEIP712Verifier_VerifyTypedData(b *testing.B) {
	ev := NewEIP712Verifier(zap.NewNop())
	privateKey, err := crypto.GenerateKey()
	require.NoError(b, err)
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	typedData := testPermitTypedData()
	sig, err := ev.SignTypedData(typedData, privateKey)
	require.NoError(b, err)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ev.VerifyTypedData(address, typedData, sig)
	}
}
