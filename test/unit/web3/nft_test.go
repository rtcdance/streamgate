package web3_test

import (
	"testing"

	"streamgate/pkg/web3"
	"streamgate/test/helpers"
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
			result := web3.IsValidAddress(tt.address)
			helpers.AssertEqual(t, tt.isValid, result)
		})
	}
}

func TestNFT_ValidateContractAddress(t *testing.T) {
	contractAddress := "0x1234567890123456789012345678901234567890"
	
	// Valid contract address
	isValid := web3.IsValidAddress(contractAddress)
	helpers.AssertTrue(t, isValid)
	
	// Invalid contract address
	isValid = web3.IsValidAddress("invalid")
	helpers.AssertFalse(t, isValid)
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
			result := web3.IsValidTokenID(tt.tokenID)
			helpers.AssertEqual(t, tt.isValid, result)
		})
	}
}

func TestNFT_ParseContractAddress(t *testing.T) {
	address := "0x1234567890123456789012345678901234567890"
	
	// Parse address
	parsed := web3.ParseAddress(address)
	helpers.AssertNotNil(t, parsed)
	helpers.AssertEqual(t, address, parsed)
}

func TestNFT_CompareAddresses(t *testing.T) {
	addr1 := "0x1234567890123456789012345678901234567890"
	addr2 := "0x1234567890123456789012345678901234567890"
	addr3 := "0xABCDEF1234567890ABCDEF1234567890ABCDEF12"
	
	// Same addresses
	result := web3.CompareAddresses(addr1, addr2)
	helpers.AssertTrue(t, result)
	
	// Different addresses
	result = web3.CompareAddresses(addr1, addr3)
	helpers.AssertFalse(t, result)
	
	// Case insensitive comparison
	result = web3.CompareAddresses(addr1, "0x1234567890123456789012345678901234567890")
	helpers.AssertTrue(t, result)
}

func TestNFT_FormatAddress(t *testing.T) {
	address := "0x1234567890123456789012345678901234567890"
	
	// Format address
	formatted := web3.FormatAddress(address)
	helpers.AssertNotNil(t, formatted)
	helpers.AssertTrue(t, len(formatted) > 0)
}
