package solana

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rtcdance/streamgate/pkg/cachetypes"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"go.uber.org/zap"
)

// SafeURIFetch is set by the parent web3 package to provide SSRF-safe URI fetching.
var SafeURIFetch func(ctx context.Context, uri string, result interface{}) error

type MetaplexVerifier struct {
	rpcClient  *rpc.Client
	httpClient *http.Client
	logger     *zap.Logger
	cache      cachetypes.CacheBackend
	cacheTTL   time.Duration
}

func NewMetaplexVerifier(rpcClient *rpc.Client, logger *zap.Logger, cache cachetypes.CacheBackend) *MetaplexVerifier {
	return &MetaplexVerifier{
		rpcClient: rpcClient,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     30 * time.Second,
			},
		},
		logger:   logger,
		cache:    cache,
		cacheTTL: 5 * time.Minute,
	}
}

func (mv *MetaplexVerifier) Close() {
	if mv.httpClient != nil && mv.httpClient.Transport != nil {
		if t, ok := mv.httpClient.Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}
}

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

type MetadataAttribute struct {
	TraitType string `json:"trait_type"`
	Value     string `json:"value"`
}

type MetadataProperties struct {
	Creators []MetadataCreator `json:"creators"`
	Files    []MetadataFile    `json:"files"`
}

type MetadataCreator struct {
	Address string `json:"address"`
	Share   int    `json:"share"`
}

type MetadataFile struct {
	URI  string `json:"uri"`
	Type string `json:"type"`
	CDN  bool   `json:"cdn"`
}

type MetadataCollection struct {
	Name   string `json:"name"`
	Family string `json:"family"`
}

type MetadataUses struct {
	UseMethod string `json:"use_method"`
	Remaining uint64 `json:"remaining"`
	Total     uint64 `json:"total"`
}

type MasterEdition struct {
	Key       uint8
	Supply    uint64
	MaxSupply uint64
}

type MetadataAccount struct {
	Key                 uint8
	UpdateAuthority     solana.PublicKey
	Mint                solana.PublicKey
	Data                MetadataData
	PrimarySaleHappened bool
	IsMutable           bool
	EditionNonce        uint8
}

type MetadataData struct {
	Name                 string
	Symbol               string
	URI                  string
	SellerFeeBasisPoints uint16
	Creators             []Creator
}

type Creator struct {
	Address  solana.PublicKey
	Verified bool
	Share    uint8
}

func (mv *MetaplexVerifier) VerifyNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error) {
	mv.logger.Debug("Verifying Metaplex NFT ownership",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress))

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return false, fmt.Errorf("invalid mint address: %w", err)
	}

	ownerPubKey, err := solana.PublicKeyFromBase58(ownerAddress)
	if err != nil {
		return false, fmt.Errorf("invalid owner address: %w", err)
	}

	cacheKey := fmt.Sprintf("metaplex:owner:%s:%s", mintAddress, ownerAddress)
	if mv.cache != nil {
		if cached, err := mv.cache.Get(cacheKey); err == nil {
			if owned, ok := cached.(bool); ok {
				return owned, nil
			}
		}
	}

	accountInfo, err := mv.getLargestTokenAccount(ctx, mintPubKey)
	if err != nil {
		return false, fmt.Errorf("failed to get token account: %w", err)
	}

	owned := accountInfo.Owner.Equals(ownerPubKey) && accountInfo.Amount > 0

	if mv.cache != nil {
		_ = mv.cache.SetWithExpiration(cacheKey, owned, mv.cacheTTL)
	}

	mv.logger.Debug("Metaplex ownership verified",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress),
		zap.Bool("owned", owned))

	return owned, nil
}

type TokenAccountInfo struct {
	Address solana.PublicKey
	Owner   solana.PublicKey
	Amount  uint64
}

func (mv *MetaplexVerifier) getLargestTokenAccount(ctx context.Context, mint solana.PublicKey) (*TokenAccountInfo, error) {
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
		_, _ = fmt.Sscanf(parsed.Parsed.Info.TokenAmount.Amount, "%d", &amount)

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

func (mv *MetaplexVerifier) GetMetadata(ctx context.Context, mintAddress string) (*MetaplexMetadata, error) {
	mv.logger.Debug("Getting Metaplex metadata",
		zap.String("mint", mintAddress))

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}

	cacheKey := fmt.Sprintf("metaplex:metadata:%s", mintAddress)
	if mv.cache != nil {
		if cached, err := mv.cache.Get(cacheKey); err == nil {
			if metadata, ok := cached.(*MetaplexMetadata); ok {
				return metadata, nil
			}
		}
	}

	metadataAccount, err := mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata account: %w", err)
	}

	metadata, err := mv.fetchMetadataFromURI(ctx, metadataAccount.Data.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	if mv.cache != nil {
		_ = mv.cache.SetWithExpiration(cacheKey, metadata, mv.cacheTTL)
	}

	return metadata, nil
}

func (mv *MetaplexVerifier) getMetadataAccount(ctx context.Context, mint solana.PublicKey) (*MetadataAccount, error) {
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

	accountInfo, err := mv.rpcClient.GetAccountInfo(ctx, metadataPDA)
	if err != nil {
		return nil, fmt.Errorf("failed to get account info: %w", err)
	}

	if accountInfo.Value == nil {
		return nil, fmt.Errorf("metadata account not found")
	}

	var metadata MetadataAccount
	if err := metadata.Deserialize(accountInfo.Value.Data.GetBinary()); err != nil {
		return nil, fmt.Errorf("failed to deserialize metadata: %w", err)
	}

	return &metadata, nil
}

func (mv *MetaplexVerifier) fetchMetadataFromURI(ctx context.Context, uri string) (*MetaplexMetadata, error) {
	var metadata MetaplexMetadata
	if SafeURIFetch != nil {
		if err := SafeURIFetch(ctx, uri, &metadata); err != nil {
			return nil, err
		}
	} else {
		if err := basicFetchURI(ctx, uri, &metadata); err != nil {
			return nil, err
		}
	}
	return &metadata, nil
}

func (mv *MetaplexVerifier) VerifyMetadata(ctx context.Context, mintAddress string, expectedMetadata *MetaplexMetadata) (bool, error) {
	mv.logger.Debug("Verifying Metaplex metadata",
		zap.String("mint", mintAddress))

	actualMetadata, err := mv.GetMetadata(ctx, mintAddress)
	if err != nil {
		return false, fmt.Errorf("failed to get metadata: %w", err)
	}

	valid := mv.compareMetadata(actualMetadata, expectedMetadata)

	mv.logger.Debug("Metaplex metadata verified",
		zap.String("mint", mintAddress),
		zap.Bool("valid", valid))

	return valid, nil
}

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

func (mv *MetaplexVerifier) VerifyCreator(ctx context.Context, mintAddress, creatorAddress string) (bool, error) {
	mv.logger.Debug("Verifying Metaplex creator",
		zap.String("mint", mintAddress),
		zap.String("creator", creatorAddress))

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return false, fmt.Errorf("invalid mint address: %w", err)
	}

	creatorPubKey, err := solana.PublicKeyFromBase58(creatorAddress)
	if err != nil {
		return false, fmt.Errorf("invalid creator address: %w", err)
	}

	metadataAccount, err := mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return false, fmt.Errorf("failed to get metadata account: %w", err)
	}

	for _, creator := range metadataAccount.Data.Creators {
		if creator.Address.Equals(creatorPubKey) && creator.Verified {
			return true, nil
		}
	}

	return false, nil
}

func (mv *MetaplexVerifier) VerifyCollection(ctx context.Context, mintAddress, collectionMintAddress string) (bool, error) {
	mv.logger.Debug("Verifying Metaplex collection",
		zap.String("mint", mintAddress),
		zap.String("collection", collectionMintAddress))

	metadata, err := mv.GetMetadata(ctx, mintAddress)
	if err != nil {
		return false, fmt.Errorf("failed to get metadata: %w", err)
	}

	if metadata.Collection == nil {
		return false, nil
	}

	collectionMintPubKey, err := solana.PublicKeyFromBase58(collectionMintAddress)
	if err != nil {
		return false, fmt.Errorf("invalid collection mint address: %w", err)
	}

	collectionPDA, _, err := solana.FindProgramAddress(
		[][]byte{
			[]byte("metadata"),
			metaplexProgramID[:],
			collectionMintPubKey[:],
		},
		metaplexProgramID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to derive collection PDA: %w", err)
	}

	accountInfo, err := mv.rpcClient.GetAccountInfo(ctx, collectionPDA)
	if err != nil || accountInfo.Value == nil {
		return false, nil
	}

	var collectionMeta MetadataAccount
	if err := collectionMeta.Deserialize(accountInfo.Value.Data.GetBinary()); err != nil {
		return false, fmt.Errorf("failed to deserialize collection metadata: %w", err)
	}

	verified := collectionMeta.Mint.Equals(collectionMintPubKey) && collectionMeta.Data.Name == metadata.Collection.Name
	return verified, nil
}

func (mv *MetaplexVerifier) IsMetaplexNFT(ctx context.Context, mintAddress string) (bool, error) {
	mv.logger.Debug("Checking if mint is Metaplex NFT",
		zap.String("mint", mintAddress))

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return false, fmt.Errorf("invalid mint address: %w", err)
	}

	_, err = mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return false, nil
	}

	return true, nil
}

type TokenInfo struct {
	ContractAddress string
	TokenID         string
	TokenType       string
	URI             string
	Metadata        map[string]interface{}
}

func (mv *MetaplexVerifier) GetTokenInfo(ctx context.Context, mintAddress string) (*TokenInfo, error) {
	mv.logger.Debug("Getting Metaplex token info",
		zap.String("mint", mintAddress))

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}

	metadataAccount, err := mv.getMetadataAccount(ctx, mintPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata account: %w", err)
	}

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

func (m *MetadataAccount) Deserialize(data []byte) error {
	if len(data) < 67 {
		return fmt.Errorf("metadata account data too short: %d bytes, need at least 67", len(data))
	}
	r := &borshReader{data: data}

	m.Key = r.readU8()
	copy(m.UpdateAuthority[:], r.readBytes(32))
	copy(m.Mint[:], r.readBytes(32))

	m.Data.Name = r.readBorshString()
	m.Data.Symbol = r.readBorshString()
	m.Data.URI = r.readBorshString()
	m.Data.SellerFeeBasisPoints = r.readU16()

	hasCreators := r.readU8()
	if hasCreators == 1 {
		count := r.readU32()
		if count > 100 {
			return fmt.Errorf("too many creators: %d", count)
		}
		m.Data.Creators = make([]Creator, count)
		for i := uint32(0); i < count; i++ {
			copy(m.Data.Creators[i].Address[:], r.readBytes(32))
			m.Data.Creators[i].Verified = r.readU8() == 1
			m.Data.Creators[i].Share = r.readU8()
		}
	}

	m.PrimarySaleHappened = r.readU8() == 1
	m.IsMutable = r.readU8() == 1

	if len(data) > r.pos {
		if edOpt := r.readU8(); edOpt == 1 {
			m.EditionNonce = r.readU8()
		}
	}

	return r.err
}

type borshReader struct {
	data []byte
	pos  int
	err  error
}

func (r *borshReader) readBytes(n int) []byte {
	if r.err != nil || r.pos+n > len(r.data) {
		if r.err == nil {
			r.err = io.ErrUnexpectedEOF
		}
		return nil
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *borshReader) readU8() uint8 {
	if b := r.readBytes(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *borshReader) readU16() uint16 {
	if b := r.readBytes(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *borshReader) readU32() uint32 {
	if b := r.readBytes(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *borshReader) readBorshString() string {
	length := r.readU32()
	if length > 1024 {
		r.err = fmt.Errorf("string too long: %d", length)
		return ""
	}
	b := r.readBytes(int(length))
	if b == nil {
		return ""
	}
	return string(b)
}

var (
	metaplexProgramID = solana.MustPublicKeyFromBase58("metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s")
)

func basicFetchURI(ctx context.Context, uri string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch URI: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}
