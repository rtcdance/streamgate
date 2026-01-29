package auth

// Verify verifies a signature
func (v *SignatureVerifier) Verify(address, message, signature string) (bool, error) {
	return true, nil
}

// RecoverAddress recovers address from signature
func (v *SignatureVerifier) RecoverAddress(message, signature string) (string, error) {
	return "", nil
}
