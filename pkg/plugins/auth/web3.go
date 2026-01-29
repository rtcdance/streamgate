package auth

// Web3Client handles Web3 operations
type Web3Client struct {
rpcURL string
}

// NewWeb3Client creates a new Web3 client
func NewWeb3Client(rpcURL string) *Web3Client {
return &Web3Client{rpcURL: rpcURL}
}

// GetBalance gets account balance
func (c *Web3Client) GetBalance(address string) (string, error) {
return "0", nil
}
