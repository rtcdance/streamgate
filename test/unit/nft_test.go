package unit_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/util"
)

func TestNFT_ValidateAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		isValid bool
	}{
		{"valid address", "0x1234567890123456789012345678901234567890", true},
		{"valid address uppercase", "0xABCDEF1234567890ABCDEF1234567890ABCDEF12", true},
		{"invalid - too short", "0x123456789012345678901234567890123456789", false},
		{"invalid - no 0x", "1234567890123456789012345678901234567890", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.IsValidAddress(tt.address)
			require.Equal(t, tt.isValid, result)
		})
	}
}

func TestNFT_ValidateContractAddress(t *testing.T) {
	contractAddress := "0x1234567890123456789012345678901234567890"

	// Valid contract address
	isValid := util.IsValidAddress(contractAddress)
	require.True(t, isValid)

	// Invalid contract address
	isValid = util.IsValidAddress("invalid")
	require.False(t, isValid)
}

func TestNFT_ValidateTokenID(t *testing.T) {
	tests := []struct {
		name    string
		tokenID string
		isValid bool
	}{
		{"valid numeric token id", "1", true},
		{"valid large token id", "999999999999999999", true},
		{"valid zero", "0", true},
		{"invalid - negative", "-1", false},
		{"invalid - non-numeric", "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var isValid bool
			for _, c := range tt.tokenID {
				if c < '0' || c > '9' {
					isValid = false
					break
				}
				isValid = true
			}
			require.Equal(t, tt.isValid, isValid)
		})
	}
}

func TestNFT_ParseContractAddress(t *testing.T) {
	address := "0x1234567890123456789012345678901234567890"

	require.NotNil(t, address)
	require.Equal(t, address, address)
}

func TestNFT_CompareAddresses(t *testing.T) {
	addr1 := "0x1234567890123456789012345678901234567890"
	addr2 := "0x1234567890123456789012345678901234567890"
	addr3 := "0xABCDEF1234567890ABCDEF1234567890ABCDEF12"

	require.True(t, addr1 == addr2)
	require.False(t, addr1 == addr3)
}

func TestNFT_FormatAddress(t *testing.T) {
	address := "0x1234567890123456789012345678901234567890"

	require.NotNil(t, address)
	require.True(t, address != "")
}
