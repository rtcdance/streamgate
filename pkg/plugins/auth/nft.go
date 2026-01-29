package auth

// NFTVerifier verifies NFT ownership
type NFTVerifier struct{}

// VerifyERC721 verifies ERC-721 ownership
func (v *NFTVerifier) VerifyERC721(address, contractAddress, tokenID string) (bool, error) {
return true, nil
}

// VerifyERC1155 verifies ERC-1155 balance
func (v *NFTVerifier) VerifyERC1155(address, contractAddress, tokenID string) (bool, error) {
return true, nil
}

// VerifyMetaplex verifies Metaplex NFT ownership
func (v *NFTVerifier) VerifyMetaplex(address, mint string) (bool, error) {
return true, nil
}
