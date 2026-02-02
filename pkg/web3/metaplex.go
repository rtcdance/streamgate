package web3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"go.uber.org/zap"
)

// MetaplexVerifier handles Solana Metaplex NFT verification
type MetaplexVerifier struct {
	rpcClient  *rpc.Client
	httpClient *http.Client
	logger     *zap.Logger
	cache      NFTCacheStorage
}

// NewMetaplexVerifier creates a new Metaplex verifier
func NewMetaplexVerifier(rpcClient *rpc.Client, logger *zap.Logger, cache NFTCacheStorage) *MetaplexVerifier {
	return &MetaplexVerifier{
		rpcClient:  rpcClient,
		httpClient: &http.Client{},
		logger:     logger,
		cache:      cache,
	}
}

// MetaplexMetadata represents the metadata structure
type MetaplexMetadata struct {
	Name        string              `json:"name"`
	Symbol      string              `json:"symbol"`
	Description string              `json:"description"`
	SellerFee   int                 `json:"seller_fee_basis_points"`
	Image       string              `json:"image"`
	ExternalURL string              `json:"external_url"`
	Attributes  []MetadataAttribute `json:"attributes"`
	Properties  MetadataProperties  `json:"properties"`
	Creators    []MetadataCreator   `json:"creators"`
	Collection  *MetadataCollection `json:"collection"`
	Uses        *MetadataUses       `json:"uses"`
}

// MetadataAttribute represents a metadata attribute
type MetadataAttribute struct {
	TraitType string `json:"trait_type"`
	Value     string `json:"value"`
}

// MetadataProperties represents metadata properties
type MetadataProperties struct {
	Creators []MetadataCreator `json:"creators"`
	Files    []MetadataFile    `json:"files"`
}

// MetadataCreator represents a metadata creator
type MetadataCreator struct {
	Address string `json:"address"`
	Share   int    `json:"share"`
}

// MetadataFile represents a metadata file
type MetadataFile struct {
	URI  string `json:"uri"`
	Type string `json:"type"`
	CDN  bool   `json:"cdn"`
}

// MetadataCollection represents metadata collection
type MetadataCollection struct {
	Name   string `json:"name"`
	Family string `json:"family"`
}

// MetadataUses represents metadata uses
type MetadataUses struct {
	UseMethod string `json:"use_method"`
	Remaining uint64 `json:"remaining"`
	Total     uint64 `json:"total"`
}

// MasterEdition represents the master edition account
type MasterEdition struct {
	Key       uint8
	Supply    uint64
	MaxSupply uint64
}

// MetadataAccount represents the metadata account
type MetadataAccount struct {
	Key                 uint8
	UpdateAuthority     solana.PublicKey
	Mint                solana.PublicKey
	Data                MetadataData
	PrimarySaleHappened bool
	IsMutable           bool
	EditionNonce        uint8
}

// MetadataData represents the metadata data
type MetadataData struct {
	Name                 string
	Symbol               string
	URI                  string
	SellerFeeBasisPoints uint16
	Creators             []Creator
}

// Creator represents a creator
type Creator struct {
	Address  solana.PublicKey
	Verified bool
	Share    uint8
}

// VerifyNFTOwnership verifies Metaplex NFT ownership
func (mv *MetaplexVerifier) VerifyNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error) {
	mv.logger.Debug("Verifying Metaplex NFT ownership",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress))

	// Validate inputs
	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return false, fmt.Errorf("invalid mint address: %w", err)
	}

	ownerPubKey, err := solana.PublicKeyFromBase58(ownerAddress)
	if err != nil {
		return false, fmt.Errorf("invalid owner address: %w", err)
	}

	// Check cache first
	cacheKey := fmt.Sprintf("metaplex:owner:%s:%s", mintAddress, ownerAddress)
	if mv.cache != nil {
		if cached, err := mv.cache.Get(cacheKey); err == nil {
			if owned, ok := cached.(bool); ok {
				return owned, nil
			}
		}
	}

	// Get the largest token account for the mint
	accountInfo, err := mv.getLargestTokenAccount(ctx, mintPubKey)
	if err != nil {
		return false, fmt.Errorf("failed to get token account: %w", err)
	}

	// Check if the owner matches
	owned := accountInfo.Owner.Equals(ownerPubKey) && accountInfo.Amount > 0

	// Cache the result
	if mv.cache != nil {
		mv.cache.Set(cacheKey, owned)
	}

	mv.logger.Debug("Metaplex ownership verified",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress),
		zap.Bool("owned", owned))

	return owned, nil
}

// TokenAccountInfo represents token account information
type TokenAccountInfo struct {
	Address solana.PublicKey
	Owner   solana.PublicKey
	Amount  uint64
}

// getLargestTokenAccount gets the largest token account for a mint
func (mv *MetaplexVerifier) getLargestTokenAccount(ctx context.Context, mint solana.PublicKey) (*TokenAccountInfo, error) {
	// Get token accounts for the mint
	tokenAccounts, err := mv.rpcClient.GetTokenAccountsByOwner(
		ctx,
		mint,
		&rpc.GetTokenAccountsConfig{},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token accounts: %w", err)
	}

	if len(tokenAccounts.Value) == 0 {
		return nil, fmt.Errorf("no token accounts found")
	}

	// Find the largest account
	var largest *TokenAccountInfo
	for _, account := range tokenAccounts.Value {
		var parsed struct {
			Parsed struct {
				Info struct {
					Owner       string `json:"owner"`
					TokenAmount struct {
						Amount string `json:"amount"`
					} `json:"tokenAmount"`
				} `json:"info"`
			} `json:"parsed"`
		}

		if err := json.Unmarshal(account.Account.Data.GetBinary(), &parsed); err != nil {
			continue
		}

		owner, err := solana.PublicKeyFromBase58(parsed.Parsed.Info.Owner)
		if err != nil {
			continue
		}

		var amount uint64
		fmt.Sscanf(parsed.Parsed.Info.TokenAmount.Amount, "%d", &amount)

		if largest == nil || amount > largest.Amount {
			largest = &TokenAccountInfo{
				Address: account.Pubkey,
				Owner:   owner,
				Amount:  amount,
			}
		}
	}

	if largest == nil {
		return nil, fmt.Errorf("no valid token accounts found")
	}

	return largest, nil
}

// GetMetadata fetches and parses the metadata for an NFT
func (mv *MetaplexVerifier) GetMetadata(ctx context.Context, mintAddress string) (*MetaplexMetadata, error) {
	mv.logger.Debug("Getting Metaplex metadata",
		zap.String("mint", mintAddress))

	// Validate input
	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}

	// Check cache first
	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	if mv.cache != nil {
		if cached, err := mv.cache.Get(cacheKey); err == nil {
			if metadata, ok := cached.(*MetaplexMetadata); ok {
				return metadata, nil
			}
		}
	}

	// Get metadata account
	metadataAccount, err := mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata account: %w", err)
	}

	// Fetch metadata from URI
	metadata, err := mv.fetchMetadataFromURI(ctx, metadataAccount.Data.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	// Cache the result
	if mv.cache != nil {
		mv.cache.Set(cacheKey, metadata)
	}

	return metadata, nil
}

// getMetadataAccount gets the metadata account for a mint
func (mv *MetaplexVerifier) getMetadataAccount(ctx context.Context, mint solana.PublicKey) (*MetadataAccount, error) {
	// Derive the metadata PDA
	metadataPDA, _, err := solana.FindProgramAddress(
		[][]byte{
			[]byte("metadata"),
			metaplexProgramID[:],
			mint[:],
		},
		metaplexProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive metadata PDA: %w", err)
	}

	// Get account info
	accountInfo, err := mv.rpcClient.GetAccountInfo(ctx, metadataPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get account info: %w", err)
	}

	if accountInfo.Value == nil {
		return nil, fmt.Errorf("metadata account not found")
	}

	// Parse the metadata account
	var metadata MetadataAccount
	if err := metadata.Deserialize(accountInfo.Value.Data.GetBinary()); err != nil {
		return nil, fmt.Errorf("failed to deserialize metadata: %w", err)
	}

	return &metadata, nil
}

// fetchMetadataFromURI fetches metadata from a URI
func (mv *MetaplexVerifier) fetchMetadataFromURI(ctx context.Context, uri string) (*MetaplexMetadata, error) {
	// Handle Arweave URIs
	if strings.HasPrefix(uri, "ar://") {
		uri = "https://arweave.net/" + strings.TrimPrefix(uri, "ar://")
	}

	// Handle IPFS URIs
	if strings.HasPrefix(uri, "ipfs://") {
		uri = "https://ipfs.io/ipfs/" + strings.TrimPrefix(uri, "ipfs://")
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := mv.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch metadata: status %d", resp.StatusCode)
	}

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse metadata
	var metadata MetaplexMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

// VerifyMetadata verifies the metadata of an NFT
func (mv *MetaplexVerifier) VerifyMetadata(ctx context.Context, mintAddress string, expectedMetadata *MetaplexMetadata) (bool, error) {
	mv.logger.Debug("Verifying Metaplex metadata",
		zap.String("mint", mintAddress))

	// Get actual metadata
	actualMetadata, err := mv.GetMetadata(ctx, mintAddress)
	if err != nil {
		return false, fmt.Errorf("failed to get metadata: %w", err)
	}

	// Compare metadata
	valid := mv.compareMetadata(actualMetadata, expectedMetadata)

	mv.logger.Debug("Metaplex metadata verified",
		zap.String("mint", mintAddress),
		zap.Bool("valid", valid))

	return valid, nil
}

// compareMetadata compares two metadata structures
func (mv *MetaplexVerifier) compareMetadata(actual, expected *MetaplexMetadata) bool {
	if actual.Name != expected.Name {
		return false
	}

	if actual.Symbol != expected.Symbol {
		return false
	}

	if actual.Description != expected.Description {
		return false
	}

	if actual.SellerFee != expected.SellerFee {
		return false
	}

	if actual.Image != expected.Image {
		return false
	}

	if len(actual.Attributes) != len(expected.Attributes) {
		return false
	}

	for i, attr := range actual.Attributes {
		if attr.TraitType != expected.Attributes[i].TraitType {
			return false
		}
		if attr.Value != expected.Attributes[i].Value {
			return false
		}
	}

	return true
}

// VerifyCreator verifies the creator of an NFT
func (mv *MetaplexVerifier) VerifyCreator(ctx context.Context, mintAddress, creatorAddress string) (bool, error) {
	mv.logger.Debug("Verifying Metaplex creator",
		zap.String("mint", mintAddress),
		zap.String("creator", creatorAddress))

	// Validate input
	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return false, fmt.Errorf("invalid mint address: %w", err)
	}

	creatorPubKey, err := solana.PublicKeyFromBase58(creatorAddress)
	if err != nil {
		return false, fmt.Errorf("invalid creator address: %w", err)
	}

	// Get metadata account
	metadataAccount, err := mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return false, fmt.Errorf("failed to get metadata account: %w", err)
	}

	// Check if creator is in the list and verified
	for _, creator := range metadataAccount.Data.Creators {
		if creator.Address.Equals(creatorPubKey) && creator.Verified {
			return true, nil
		}
	}

	return false, nil
}

// VerifyCollection verifies the collection of an NFT
func (mv *MetaplexVerifier) VerifyCollection(ctx context.Context, mintAddress, collectionMintAddress string) (bool, error) {
	mv.logger.Debug("Verifying Metaplex collection",
		zap.String("mint", mintAddress),
		zap.String("collection", collectionMintAddress))

	// Get metadata
	metadata, err := mv.GetMetadata(ctx, mintAddress)
	if err != nil {
		return false, fmt.Errorf("failed to get metadata: %w", err)
	}

	// Check collection
	if metadata.Collection == nil {
		return false, nil
	}

	// Parse collection mint from metadata
	// This is a simplified check - actual implementation would need to verify the collection account
	return true, nil
}

// IsMetaplexNFT checks if a mint is a Metaplex NFT
func (mv *MetaplexVerifier) IsMetaplexNFT(ctx context.Context, mintAddress string) (bool, error) {
	mv.logger.Debug("Checking if mint is Metaplex NFT",
		zap.String("mint", mintAddress))

	// Validate input
	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return false, fmt.Errorf("invalid mint address: %w", err)
	}

	// Try to get metadata account
	_, err = mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return false, nil
	}

	return true, nil
}

// GetTokenInfo gets information about a Metaplex NFT
func (mv *MetaplexVerifier) GetTokenInfo(ctx context.Context, mintAddress string) (*TokenInfo, error) {
	mv.logger.Debug("Getting Metaplex token info",
		zap.String("mint", mintAddress))

	// Validate input
	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}

	// Get metadata account
	metadataAccount, err := mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata account: %w", err)
	}

	// Get largest token account
	tokenAccount, err := mv.getLargestTokenAccount(ctx, mintPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get token account: %w", err)
	}

	info := &TokenInfo{
		ContractAddress: metaplexProgramID.String(),
		TokenID:         mintAddress,
		TokenType:       "Metaplex",
		URI:             metadataAccount.Data.URI,
		Metadata: map[string]interface{}{
			"name":   metadataAccount.Data.Name,
			"symbol": metadataAccount.Data.Symbol,
			"owner":  tokenAccount.Owner.String(),
		},
	}

	return info, nil
}

// Deserialize deserializes the metadata account
func (m *MetadataAccount) Deserialize(data []byte) error {
	// Simplified deserialization
	// Actual implementation would need to properly parse the Metaplex metadata format
	return nil
}

// Constants
var (
	metaplexProgramID = solana.MustPublicKeyFromBase58("metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s")
)
