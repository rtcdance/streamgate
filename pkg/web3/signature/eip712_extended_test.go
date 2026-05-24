package signature

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCreateDelegationTypedData(t *testing.T) {
	domain := EIP712Domain{
		Name:              "StreamGate",
		Version:           "1",
		ChainId:           big.NewInt(1),
		VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
	}

	td := CreateDelegationTypedData(
		domain,
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		big.NewInt(1),
		42,
	)

	assert.Equal(t, "Delegation", td.PrimaryType)
	assert.Equal(t, "StreamGate", td.Domain.Name)
	assert.Contains(t, td.Types, "EIP712Domain")
	assert.Contains(t, td.Types, "Delegation")

	delegationTypes := td.Types["Delegation"]
	assert.Equal(t, 4, len(delegationTypes))

	fieldNames := make([]string, len(delegationTypes))
	for i, f := range delegationTypes {
		fieldNames[i] = f.Name
	}
	assert.Contains(t, fieldNames, "delegatee")
	assert.Contains(t, fieldNames, "authority")
	assert.Contains(t, fieldNames, "chainId")
	assert.Contains(t, fieldNames, "nonce")

	assert.Equal(t, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", td.Message["delegatee"])
	assert.Equal(t, "0x8ba1f109551bD432803012645Ac136ddd64DBA72", td.Message["authority"])
	assert.Equal(t, "1", td.Message["chainId"])
	assert.Equal(t, "42", td.Message["nonce"])
}

func TestCreateDelegationTypedData_SignAndVerify(t *testing.T) {
	ev := NewEIP712Verifier(zap.NewNop())
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	domain := EIP712Domain{
		Name:              "StreamGate",
		Version:           "1",
		ChainId:           big.NewInt(1),
		VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
	}

	td := CreateDelegationTypedData(domain, address, "0x8ba1f109551bD432803012645Ac136ddd64DBA72", big.NewInt(1), 0)

	signature, err := ev.SignTypedData(td, privateKey)
	require.NoError(t, err)

	valid, err := ev.VerifyTypedData(address, td, signature)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestParseTypedDataFromJSON_Success(t *testing.T) {
	domain := EIP712Domain{
		Name:              "StreamGate",
		Version:           "1",
		ChainId:           big.NewInt(1),
		VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
	}
	td := CreatePermitTypedData(domain, "0x1", "0x2", big.NewInt(100), big.NewInt(0), big.NewInt(1716000000))

	jsonData, err := json.Marshal(td)
	require.NoError(t, err)

	parsed, err := ParseTypedDataFromJSON(jsonData)
	require.NoError(t, err)
	assert.Equal(t, "Permit", parsed.PrimaryType)
	assert.Equal(t, "StreamGate", parsed.Domain.Name)
}

func TestParseTypedDataFromJSON_InvalidJSON(t *testing.T) {
	_, err := ParseTypedDataFromJSON([]byte("not json"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse typed data JSON")
}

func TestEncodeType(t *testing.T) {
	types := []EIP712Type{
		{Name: "owner", Type: "address"},
		{Name: "spender", Type: "address"},
		{Name: "value", Type: "uint256"},
	}

	result := EncodeType("Permit", types)
	assert.Equal(t, "Permit(address owner,address spender,uint256 value)", result)
}

func TestTypeHash(t *testing.T) {
	types := []EIP712Type{
		{Name: "owner", Type: "address"},
		{Name: "value", Type: "uint256"},
	}
	hash := TypeHash("Permit", types)
	assert.Len(t, hash, 32)
}

func TestHashStruct(t *testing.T) {
	td := testPermitTypedData()
	hash, err := HashStruct(td.PrimaryType, td.Types, td.Message)
	require.NoError(t, err)
	assert.Len(t, hash, 32)
}

func TestHashStruct_MissingField(t *testing.T) {
	types := EIP712Types{
		"Test": []EIP712Type{{Name: "field", Type: "uint256"}},
	}
	_, err := HashStruct("Test", types, map[string]interface{}{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing value for field")
}

func TestIsReferenceType(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		expected bool
	}{
		{"address", "address", false},
		{"bool", "bool", false},
		{"string", "string", false},
		{"bytes", "bytes", false},
		{"bytes32", "bytes32", false},
		{"uint256", "uint256", false},
		{"int128", "int128", false},
		{"custom_struct", "MyStruct", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isReferenceType(tc.typeName))
		})
	}
}

func TestHashValue_Address(t *testing.T) {
	result, err := hashValue("address", EIP712Types{}, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	assert.Len(t, result, 20)
}

func TestHashValue_AddressNoPrefix(t *testing.T) {
	result, err := hashValue("address", EIP712Types{}, "742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	assert.Len(t, result, 20)
}

func TestHashValue_AddressWrongType(t *testing.T) {
	_, err := hashValue("address", EIP712Types{}, 123)
	assert.Error(t, err)
}

func TestHashValue_Bool(t *testing.T) {
	result, err := hashValue("bool", EIP712Types{}, true)
	require.NoError(t, err)
	assert.Len(t, result, 32)
	assert.Equal(t, byte(1), result[31])

	result, err = hashValue("bool", EIP712Types{}, false)
	require.NoError(t, err)
	assert.Equal(t, byte(0), result[31])
}

func TestHashValue_BoolWrongType(t *testing.T) {
	_, err := hashValue("bool", EIP712Types{}, "true")
	assert.Error(t, err)
}

func TestHashValue_String(t *testing.T) {
	result, err := hashValue("string", EIP712Types{}, "hello")
	require.NoError(t, err)
	assert.Len(t, result, 32)
}

func TestHashValue_StringWrongType(t *testing.T) {
	_, err := hashValue("string", EIP712Types{}, 123)
	assert.Error(t, err)
}

func TestHashValue_Bytes(t *testing.T) {
	result, err := hashValue("bytes", EIP712Types{}, []byte{0x01, 0x02})
	require.NoError(t, err)
	assert.Len(t, result, 32)
}

func TestHashValue_BytesFromString(t *testing.T) {
	result, err := hashValue("bytes", EIP712Types{}, "0x0102")
	require.NoError(t, err)
	assert.Len(t, result, 32)
}

func TestHashValue_BytesWrongType(t *testing.T) {
	_, err := hashValue("bytes", EIP712Types{}, 123)
	assert.Error(t, err)
}

func TestHashValue_FixedBytes(t *testing.T) {
	result, err := hashValue("bytes32", EIP712Types{}, make([]byte, 32))
	require.NoError(t, err)
	assert.Len(t, result, 32)
}

func TestHashValue_FixedBytesFromString(t *testing.T) {
	result, err := hashValue("bytes4", EIP712Types{}, "0x01020304")
	require.NoError(t, err)
	assert.Len(t, result, 32)
}

func TestHashValue_Uint256(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{"big_int", big.NewInt(42)},
		{"string", "42"},
		{"float64", float64(42)},
		{"int", 42},
		{"int64", int64(42)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := hashValue("uint256", EIP712Types{}, tc.value)
			require.NoError(t, err)
			assert.Len(t, result, 32)
		})
	}
}

func TestHashValue_UintWrongType(t *testing.T) {
	_, err := hashValue("uint256", EIP712Types{}, []byte{1})
	assert.Error(t, err)
}

func TestHashValue_ReferenceType(t *testing.T) {
	types := EIP712Types{
		"Inner": []EIP712Type{{Name: "val", Type: "uint256"}},
	}
	result, err := hashValue("Inner", types, map[string]interface{}{"val": big.NewInt(1)})
	require.NoError(t, err)
	assert.Len(t, result, 32)
}

func TestHashValue_ReferenceTypeWrongValue(t *testing.T) {
	types := EIP712Types{
		"Inner": []EIP712Type{{Name: "val", Type: "uint256"}},
	}
	_, err := hashValue("Inner", types, "not a map")
	assert.Error(t, err)
}

func TestHashValue_UnsupportedType(t *testing.T) {
	_, err := hashValue("unknown_type", EIP712Types{}, "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected map for reference type")
}

func TestEncodeData(t *testing.T) {
	td := testPermitTypedData()
	result, err := EncodeData(td.PrimaryType, td.Types, td.Message)
	require.NoError(t, err)
	assert.Len(t, result, 32)
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		name      string
		typeName  string
		expected  int
		expectErr bool
	}{
		{"uint256", "uint256", 256, false},
		{"uint8", "uint8", 8, false},
		{"int128", "int128", 128, false},
		{"bytes32", "bytes32", 32, false},
		{"bytes1", "bytes1", 1, false},
		{"invalid", "uint", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			size, err := parseSize(tc.typeName)
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, size)
			}
		})
	}
}

func TestValidateSIWEMessage(t *testing.T) {
	checksummed := "0x71C7656EC7ab88b098defB751B7401B5f6d8976F"

	tests := []struct {
		name            string
		msg             *SIWEMessage
		expectedDomain  string
		expectedAddress string
		expectedNonce   string
		expectedChainID int64
		expectErr       bool
		errContains     string
	}{
		{
			name: "valid",
			msg: &SIWEMessage{
				Version: "1",
				Address: checksummed,
				Domain:  "streamgate.io",
				Nonce:   "abc123",
				ChainID: 1,
			},
			expectedDomain:  "streamgate.io",
			expectedAddress: checksummed,
			expectedNonce:   "abc123",
			expectedChainID: 1,
			expectErr:       false,
		},
		{
			name: "wrong_version",
			msg: &SIWEMessage{
				Version: "2",
				Address: checksummed,
			},
			expectErr:   true,
			errContains: "invalid SIWE version",
		},
		{
			name: "empty_address",
			msg: &SIWEMessage{
				Version: "1",
				Address: "",
			},
			expectErr:   true,
			errContains: "invalid SIWE address",
		},
		{
			name: "non_checksummed_address",
			msg: &SIWEMessage{
				Version: "1",
				Address: "0x71c7656ec7ab88b098defb751b7401b5f6d8976f",
			},
			expectErr:   true,
			errContains: "not EIP-55 checksummed",
		},
		{
			name: "domain_mismatch",
			msg: &SIWEMessage{
				Version: "1",
				Address: checksummed,
				Domain:  "evil.com",
			},
			expectedDomain: "streamgate.io",
			expectErr:      true,
			errContains:    "domain mismatch",
		},
		{
			name: "domain_skip_when_empty",
			msg: &SIWEMessage{
				Version: "1",
				Address: checksummed,
				Domain:  "anything.com",
			},
			expectedDomain: "",
			expectErr:      false,
		},
		{
			name: "nonce_mismatch",
			msg: &SIWEMessage{
				Version: "1",
				Address: checksummed,
				Nonce:   "wrong",
			},
			expectedNonce: "abc123",
			expectErr:     true,
			errContains:   "nonce mismatch",
		},
		{
			name: "chain_id_mismatch",
			msg: &SIWEMessage{
				Version: "1",
				Address: checksummed,
				ChainID: 137,
			},
			expectedChainID: 1,
			expectErr:       true,
			errContains:     "chain ID mismatch",
		},
		{
			name: "chain_id_skip_when_zero",
			msg: &SIWEMessage{
				Version: "1",
				Address: checksummed,
				ChainID: 999,
			},
			expectedChainID: 0,
			expectErr:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSIWEMessage(tc.msg, tc.expectedDomain, tc.expectedAddress, tc.expectedNonce, tc.expectedChainID)
			if tc.expectErr {
				assert.Error(t, err)
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSIWEMessageOptions(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	notBefore := now.Add(1 * time.Minute)

	msg := NewSIWEMessage("test.com", "0x71C7656EC7ab88b098defB751B7401B5f6d8976F", "https://test.com", 1, "nonce", now,
		WithSIWENotBefore(notBefore),
		WithSIWERequestID("req-123"),
		WithSIWEResources([]string{"https://test.com/resource"}),
	)

	assert.Equal(t, notBefore.UTC().Format(time.RFC3339), msg.NotBefore)
	assert.Equal(t, "req-123", msg.RequestID)
	assert.Contains(t, msg.Resources, "https://test.com/resource")
}

func TestParseSIWEMessage_InvalidAddress(t *testing.T) {
	raw := `example.com wants you to sign in with your Ethereum account:
not_an_address

URI: https://example.com
Version: 1
Chain ID: 1
Nonce: abc
Issued At: 2026-01-01T00:00:00Z`

	_, err := ParseSIWEMessage(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Ethereum address")
}

func TestParseSIWEMessage_MissingVersion(t *testing.T) {
	raw := `example.com wants you to sign in with your Ethereum account:
0x71C7656EC7ab88b098defB751B7401B5f6d8976F

Sign in to StreamGate

URI: https://example.com
Chain ID: 1
Nonce: abc
Issued At: 2026-01-01T00:00:00Z`

	_, err := ParseSIWEMessage(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing Version")
}

func TestParseSIWEMessage_MissingNonce(t *testing.T) {
	raw := `example.com wants you to sign in with your Ethereum account:
0x71C7656EC7ab88b098defB751B7401B5f6d8976F

Sign in to StreamGate

URI: https://example.com
Version: 1
Chain ID: 1
Issued At: 2026-01-01T00:00:00Z`

	_, err := ParseSIWEMessage(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing Nonce")
}
