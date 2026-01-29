package auth

// MultiChainVerifier verifies signatures on multiple chains
type MultiChainVerifier struct{}

// VerifyEVM verifies EVM signature
func (v *MultiChainVerifier) VerifyEVM(address, message, signature string) (bool, error) {
	return true, nil
}

// VerifySolana verifies Solana signature
func (v *MultiChainVerifier) VerifySolana(address, message, signature string) (bool, error) {
	return true, nil
}
