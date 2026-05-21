package web3

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/cachetypes"
	"github.com/rtcdance/streamgate/pkg/web3/event"
	"github.com/rtcdance/streamgate/pkg/web3/nft"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"github.com/rtcdance/streamgate/pkg/web3/solana"
	"github.com/rtcdance/streamgate/pkg/web3/tx"
	"go.uber.org/zap"
)

type (
	SolanaVerifier    = solana.SolanaVerifier
	MetaplexMetadata  = solana.MetaplexMetadata
	EIP712TypedData   = signature.EIP712TypedData
	EIP712Domain      = signature.EIP712Domain
	EIP712Types       = signature.EIP712Types
	SignatureVerifier = signature.SignatureVerifier
	WalletManager     = signature.WalletManager
	SecurePrivateKey  = signature.SecurePrivateKey
	SIWEMessage       = signature.SIWEMessage
	SIWEMessageOption = signature.SIWEMessageOption
	EIP712Verifier    = signature.EIP712Verifier
	EthCaller         = nft.EthCaller
	BlockTagCaller    = nft.BlockTagCaller
	NFTVerifier       = nft.NFTVerifier
	NFTInfo           = nft.NFTInfo
	ApprovalInfo      = nft.ApprovalInfo
	TokenStandard     = nft.TokenStandard
	ERC1155Verifier   = nft.ERC1155Verifier
	ERC20Reader       = nft.ERC20Reader
	ERC20TokenInfo    = nft.ERC20TokenInfo
	BlockTag            = nft.BlockTag
	PendingTx           = tx.PendingTx
	TrackedTx           = tx.TrackedTx
	TxStatus            = tx.TxStatus
	ContractWriteResult = tx.ContractWriteResult
	ContractTxOpts      = tx.ContractTxOpts
	ContractWriter      = tx.ContractWriter
	KeyProvider         = tx.KeyProvider
	NonceProvider       = tx.NonceProvider
	NonceClient         = tx.NonceClient
	NonceManager        = tx.NonceManager
	TxTracker           = tx.TxTracker
	TxLifecycleManager  = tx.TxLifecycleManager
	TxLifecycleConfig   = tx.TxLifecycleConfig
	TxClient            = tx.Client
)

const (
	TokenStandardUnknown = nft.TokenStandardUnknown
	TokenStandardERC721  = nft.TokenStandardERC721
	TokenStandardERC1155 = nft.TokenStandardERC1155
	BlockTagLatest       = nft.BlockTagLatest
	BlockTagSafe         = nft.BlockTagSafe
	BlockTagFinalized    = nft.BlockTagFinalized
)

var (
	ERC20ABI = nft.ERC20ABI
)

const PermitABI = nft.PermitABI

func NewNFTVerifier(client EthCaller, logger *zap.Logger) *NFTVerifier {
	return nft.NewNFTVerifier(client, logger)
}

func NewERC1155Verifier(ethClient EthCaller, logger *zap.Logger, cache cachetypes.CacheBackend) *ERC1155Verifier {
	return nft.NewERC1155Verifier(ethClient, logger, cache)
}

func NewERC20Reader(caller EthCaller, logger *zap.Logger) *ERC20Reader {
	return nft.NewERC20Reader(caller, logger)
}

func DetectTokenStandard(ctx context.Context, caller EthCaller, contractAddress string, logger *zap.Logger) TokenStandard {
	return nft.DetectTokenStandard(ctx, caller, contractAddress, logger)
}

func PackPermitCall(owner, spender common.Address, value, deadline *big.Int, v uint8, r, s [32]byte) ([]byte, error) {
	return nft.PackPermitCall(owner, spender, value, deadline, v, r, s)
}

func NewSIWEMessage(domain, address, uri string, chainID int64, nonce string, issuedAt time.Time, opts ...SIWEMessageOption) *SIWEMessage {
	return signature.NewSIWEMessage(domain, address, uri, chainID, nonce, issuedAt, opts...)
}

func WithSIWEExpirationTime(t time.Time) SIWEMessageOption {
	return signature.WithSIWEExpirationTime(t)
}

func BuildSIWEMessage(msg *SIWEMessage) string {
	return signature.BuildSIWEMessage(msg)
}

func ParseSIWEMessage(message string) (*SIWEMessage, error) {
	return signature.ParseSIWEMessage(message)
}

func ValidateSIWEMessage(msg *SIWEMessage, expectedDomain, expectedAddress, expectedNonce string, expectedChainID int64) error {
	return signature.ValidateSIWEMessage(msg, expectedDomain, expectedAddress, expectedNonce, expectedChainID)
}

func NewSignatureVerifier(logger *zap.Logger) *SignatureVerifier {
	return signature.NewSignatureVerifier(logger)
}

func NewEIP712Verifier(logger *zap.Logger) *EIP712Verifier {
	return signature.NewEIP712Verifier(logger)
}

func NewWalletManager(logger *zap.Logger) *WalletManager {
	return signature.NewWalletManager(logger)
}

func NewSecurePrivateKeyFromHex(hexKey string) (*SecurePrivateKey, error) {
	return signature.NewSecurePrivateKeyFromHex(hexKey)
}

func NewSolanaVerifier(logger *zap.Logger, rpcEndpoint ...string) *SolanaVerifier {
	return solana.NewSolanaVerifier(logger, rpcEndpoint...)
}

type ChainReader interface {
	GetClient(chainID int64) (*ChainClient, error)
	GetSolanaClient(chainID int64) (*SolanaVerifier, error)
	GetSupportedChains() []*ChainConfig
	GetRPCStatuses() map[int64][]RPCStatus
	GetTestnetChains() []*ChainConfig
	GetMainnetChains() []*ChainConfig
}

type ChainAdmin interface {
	AddChain(chainID int64) error
	SetRateLimiter(rl *RPCRateLimiter)
}

type ChainLifecycle interface {
	Close()
}

type ChainManagerInterface interface {
	ChainReader
	ChainAdmin
	ChainLifecycle
}

type SignatureVerifierInterface interface {
	VerifySignature(ctx context.Context, address, message, signature string) (bool, error)
}

type EIP712VerifierInterface interface {
	VerifyTypedData(address string, typedData *EIP712TypedData, signature string) (bool, error)
}

// SolanaSigner verifies Solana ed25519 signatures (purely local crypto, no RPC).
type SolanaSigner interface {
	VerifySignature(address, message, signature string) (bool, error)
	VerifyOffchainMessage(address, message, signature string) (bool, error)
}

type SolanaVerifierInterface interface {
	VerifyTokenAccount(ctx context.Context, tokenAccount, ownerAddress string) (bool, error)
	VerifyMintAuthority(ctx context.Context, mintAddress, authorityAddress string) (bool, error)
	VerifyMetaplexNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error)
	FetchMetaplexMetadata(ctx context.Context, mintAddress string) (*MetaplexMetadata, error)
	Close()
}

type EventIndexer = event.EventIndexer
type EventIndexerConfig = event.EventIndexerConfig
type IndexedEvent = event.IndexedEvent
type EventReader = event.EventReader
type EventHandler = event.EventHandler
type EventListener = event.EventListener
type EventStore = event.EventStore
type MemoryEventStore = event.MemoryEventStore
type EventParser = event.EventParser
type ParsedEvent = event.ParsedEvent
type ContentRegisteredEvent = event.ContentRegisteredEvent
type NFTMintedEvent = event.NFTMintedEvent
type LogSubscriberInterface = event.LogSubscriberInterface
type ReorgChecker = event.ReorgChecker

func NewEventIndexer(client EventReader, contractAddress, eventSignature string, logger *zap.Logger) (*EventIndexer, error) {
	return event.NewEventIndexer(client, contractAddress, eventSignature, logger)
}

func NewEventIndexerWithConfig(client EventReader, cfg EventIndexerConfig, logger *zap.Logger) (*EventIndexer, error) {
	return event.NewEventIndexerWithConfig(client, cfg, logger)
}

func NewMemoryEventStore() *MemoryEventStore {
	return event.NewMemoryEventStore()
}

func NewEventParser(logger *zap.Logger) *EventParser {
	return event.NewEventParser(logger)
}

func NewEventListener(indexer *EventIndexer, logger *zap.Logger) *EventListener {
	return event.NewEventListener(indexer, logger)
}

func DecodeContentRegisteredEvent(e *IndexedEvent) (*ContentRegisteredEvent, error) {
	return event.DecodeContentRegisteredEvent(e)
}

func DecodeNFTMintedEvent(e *IndexedEvent) (*NFTMintedEvent, error) {
	return event.DecodeNFTMintedEvent(e)
}


func DefaultTxLifecycleConfig() tx.TxLifecycleConfig {
	return tx.DefaultTxLifecycleConfig()
}

func NewTxLifecycleManager(client tx.Client, keyProvider tx.KeyProvider, config tx.TxLifecycleConfig, logger *zap.Logger) *tx.TxLifecycleManager {
	return tx.NewTxLifecycleManager(client, keyProvider, config, logger)
}

func NewNonceManager(client tx.NonceClient, logger *zap.Logger) *tx.NonceManager {
	return tx.NewNonceManager(client, logger)
}

func NewTxTracker(client tx.Client, logger *zap.Logger) *tx.TxTracker {
	return tx.NewTxTracker(client, logger)
}
