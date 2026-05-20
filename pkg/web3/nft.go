package web3

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// EthCaller abstracts the Ethereum contract call interface.
// *ethclient.Client satisfies this interface implicitly.
//
//go:generate mockgen -destination=mocks/mock_eth_caller.go -package=mocks streamgate/pkg/web3 EthCaller
type EthCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

type tokenStandardEntry struct {
	standard TokenStandard
	cachedAt time.Time
}

const tokenStandardCacheTTL = 1 * time.Hour

// NFTVerifier handles NFT verification
type NFTVerifier struct {
	client         EthCaller
	logger         *zap.Logger
	erc721ABI      abi.ABI  // pre-parsed at construction
	erc721MetaABI  abi.ABI  // pre-parsed: name, symbol, tokenURI, ownerOf
	erc1155ABI     abi.ABI  // pre-parsed: balanceOf(address,uint256)
	KnownOperators []string // known marketplace/operator contracts to check isApprovedForAll
	blockTag       BlockTag // block tag for reading state (default: BlockTagLatest)
	standardCache  sync.Map // contractAddress → *tokenStandardEntry
}

// BlockTagCaller is an optional interface that EthCaller implementations
// (e.g., ChainClient) can satisfy to support reading state at specific
// block tags (safe, finalized) for reorg protection.
type BlockTagCaller interface {
	CallContractAtBlock(ctx context.Context, msg ethereum.CallMsg, blockTag BlockTag) ([]byte, error)
}

// DefaultKnownOperators contains well-known NFT marketplace contract addresses
// (Ethereum mainnet). These operators can transfer NFTs via isApprovedForAll.
var DefaultKnownOperators = []string{
	"0x000000a95b0a88b3E564F5e5101CE5F02393a849", // OpenSea Seaport 1.6
	"0x2a5c4E3CE3dBD1686e6c1A57a1cE6253a2B96480", // OpenSea Seaport 1.5
	"0x59728544B08AB483533076417FbBB2fD0B17ce3a", // OpenSea Wyvern Exchange
	"0xF5b12ABAa25696301A4b991c0D9f6B4A5c297383", // Blur
	"0xF849de01B2133aC8d9a1f89B175A02ad0a2f4394", // X2Y2
}

// erc721ABIJSON contains the ERC-721 methods used by NFTVerifier.
const erc721ABIJSON = `[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"operator","type":"address"}],"name":"isApprovedForAll","outputs":[{"name":"","type":"bool"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"getApproved","outputs":[{"name":"","type":"address"}],"type":"function"}]`

// erc721MetaABIJSON contains ERC-721 metadata methods for GetNFTInfo.
const erc721MetaABIJSON = `[{"constant":true,"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`

const erc1155ABIJSON = `[{"constant":true,"inputs":[{"name":"account","type":"address"},{"name":"id","type":"uint256"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},{"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"operator","type":"address"}],"name":"isApprovedForAll","outputs":[{"name":"","type":"bool"}],"type":"function"}]`

var (
	preparsedERC721ABI     = mustParseABI("ERC-721", erc721ABIJSON)
	preparsedERC721MetaABI = mustParseABI("ERC-721-meta", erc721MetaABIJSON)
	preparsedERC1155ABI    = mustParseABI("ERC-1155", erc1155ABIJSON)
)

// NewNFTVerifier creates a new NFT verifier
func NewNFTVerifier(client EthCaller, logger *zap.Logger) *NFTVerifier {
	return &NFTVerifier{
		client:        client,
		logger:        logger,
		erc721ABI:     preparsedERC721ABI,
		erc721MetaABI: preparsedERC721MetaABI,
		erc1155ABI:    preparsedERC1155ABI,
	}
}

// WithBlockTag sets the block tag for contract reads. Using BlockTagSafe or
// BlockTagFinalized protects against reorgs at the cost of reading slightly
// stale state. Returns the verifier for chaining.
func (nv *NFTVerifier) WithBlockTag(tag BlockTag) *NFTVerifier {
	nv.blockTag = tag
	return nv
}

// VerifyNFTOwnership verifies if an address owns an NFT
func (nv *NFTVerifier) VerifyNFTOwnership(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	nv.logger.Debug("Verifying NFT ownership",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	// Parse addresses
	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)

	// Parse token ID
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return false, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	// Call ownerOf
	data, err := nv.erc721ABI.Pack("ownerOf", tokenIDInt)
	if err != nil {
		nv.logger.Error("Failed to pack ownerOf call", zap.Error(err))
		return false, fmt.Errorf("failed to pack ownerOf call: %w", err)
	}

	result, err := nv.callContract(ctx, contract, data)
	if err != nil {
		nv.logger.Error("Failed to call ownerOf", zap.Error(err))
		return false, fmt.Errorf("failed to call ownerOf: %w", err)
	}

	if len(result) < 32 {
		return false, fmt.Errorf("ownerOf returned insufficient data (len=%d): contract may not exist or is not a valid ERC-721 contract", len(result))
	}

	// Unpack result
	var tokenOwner common.Address
	err = nv.erc721ABI.UnpackIntoInterface(&tokenOwner, "ownerOf", result)
	if err != nil {
		nv.logger.Error("Failed to unpack ownerOf result", zap.Error(err))
		return false, fmt.Errorf("failed to unpack ownerOf result: %w", err)
	}

	// Compare addresses
	isOwner := tokenOwner == owner
	nv.logger.Debug("NFT ownership verified",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.Bool("is_owner", isOwner))

	return isOwner, nil
}

// GetNFTBalance gets the NFT balance of an address
func (nv *NFTVerifier) GetNFTBalance(ctx context.Context, contractAddress, ownerAddress string) (*big.Int, error) {
	nv.logger.Debug("Getting NFT balance",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress))

	// Parse addresses
	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)

	// Call balanceOf
	data, err := nv.erc721ABI.Pack("balanceOf", owner)
	if err != nil {
		nv.logger.Error("Failed to pack balanceOf call", zap.Error(err))
		return nil, fmt.Errorf("failed to pack balanceOf call: %w", err)
	}

	result, err := nv.callContract(ctx, contract, data)
	if err != nil {
		nv.logger.Error("Failed to call balanceOf", zap.Error(err))
		return nil, fmt.Errorf("failed to call balanceOf: %w", err)
	}

	if len(result) < 32 {
		return nil, fmt.Errorf("balanceOf returned insufficient data (len=%d): contract may not exist or is not a valid ERC-721 contract", len(result))
	}

	// Unpack result
	var balance *big.Int
	err = nv.erc721ABI.UnpackIntoInterface(&balance, "balanceOf", result)
	if err != nil {
		nv.logger.Error("Failed to unpack balanceOf result", zap.Error(err))
		return nil, fmt.Errorf("failed to unpack balanceOf result: %w", err)
	}

	nv.logger.Debug("NFT balance retrieved",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress),
		zap.String("balance", balance.String()))
	return balance, nil
}

// NFTInfo contains NFT information
type NFTInfo struct {
	ContractAddress string
	TokenID         string
	Owner           string
	Name            string
	Symbol          string
	URI             string
	Warnings        []string
}

// GetNFTInfo gets information about an NFT including name, symbol, and tokenURI.
// These metadata calls are best-effort: if a call reverts (non-standard contract),
// the field is left empty and no error is returned.
func (nv *NFTVerifier) GetNFTInfo(ctx context.Context, contractAddress, tokenID string) (*NFTInfo, error) {
	nv.logger.Debug("Getting NFT info",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID))

	contract := common.HexToAddress(contractAddress)
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return nil, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	nftInfo := &NFTInfo{
		ContractAddress: contractAddress,
		TokenID:         tokenID,
	}

	if data, err := nv.erc721MetaABI.Pack("name"); err == nil {
		if result, err := nv.callContract(ctx, contract, data); err == nil {
			if len(result) >= 32 {
				var name string
				if err := nv.erc721MetaABI.UnpackIntoInterface(&name, "name", result); err == nil {
					nftInfo.Name = name
				}
			}
		} else {
			nftInfo.Warnings = append(nftInfo.Warnings, fmt.Sprintf("name: %s", err.Error()))
		}
	}

	if data, err := nv.erc721MetaABI.Pack("symbol"); err == nil {
		if result, err := nv.callContract(ctx, contract, data); err == nil {
			if len(result) >= 32 {
				var symbol string
				if err := nv.erc721MetaABI.UnpackIntoInterface(&symbol, "symbol", result); err == nil {
					nftInfo.Symbol = symbol
				}
			}
		} else {
			nftInfo.Warnings = append(nftInfo.Warnings, fmt.Sprintf("symbol: %s", err.Error()))
		}
	}

	if data, err := nv.erc721MetaABI.Pack("tokenURI", tokenIDInt); err == nil {
		if result, err := nv.callContract(ctx, contract, data); err == nil {
			if len(result) >= 32 {
				var tokenURI string
				if err := nv.erc721MetaABI.UnpackIntoInterface(&tokenURI, "tokenURI", result); err == nil {
					nftInfo.URI = tokenURI
				}
			}
		} else {
			nftInfo.Warnings = append(nftInfo.Warnings, fmt.Sprintf("tokenURI: %s", err.Error()))
		}
	}

	if data, err := nv.erc721MetaABI.Pack("ownerOf", tokenIDInt); err == nil {
		if result, err := nv.callContract(ctx, contract, data); err == nil {
			if len(result) >= 32 {
				var owner common.Address
				if err := nv.erc721MetaABI.UnpackIntoInterface(&owner, "ownerOf", result); err == nil {
					nftInfo.Owner = owner.Hex()
				}
			}
		} else {
			nftInfo.Warnings = append(nftInfo.Warnings, fmt.Sprintf("ownerOf: %s", err.Error()))
		}
	}

	nv.logger.Debug("NFT info retrieved",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("name", nftInfo.Name),
		zap.String("symbol", nftInfo.Symbol))
	return nftInfo, nil
}

// VerifyNFTCollection verifies if an address owns any NFT from a collection
func (nv *NFTVerifier) VerifyNFTCollection(ctx context.Context, contractAddress, ownerAddress string) (bool, error) {
	nv.logger.Debug("Verifying NFT collection ownership",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress))

	balance, err := nv.GetNFTBalance(ctx, contractAddress, ownerAddress)
	if err != nil {
		return false, err
	}

	hasNFT := balance.Cmp(big.NewInt(0)) > 0
	nv.logger.Debug("NFT collection ownership verified",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress),
		zap.Bool("has_nft", hasNFT))

	return hasNFT, nil
}

// VerifyERC1155Ownership verifies ERC-1155 token ownership via balanceOf(address,uint256).
// Returns true if the account holds a non-zero balance of the given token ID.
func (nv *NFTVerifier) VerifyERC1155Ownership(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	nv.logger.Debug("Verifying ERC-1155 ownership",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return false, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	data, err := nv.erc1155ABI.Pack("balanceOf", owner, tokenIDInt)
	if err != nil {
		nv.logger.Error("Failed to pack ERC-1155 balanceOf call", zap.Error(err))
		return false, fmt.Errorf("failed to pack ERC-1155 balanceOf call: %w", err)
	}

	result, err := nv.callContract(ctx, contract, data)
	if err != nil {
		nv.logger.Error("Failed to call ERC-1155 balanceOf", zap.Error(err))
		return false, fmt.Errorf("failed to call ERC-1155 balanceOf: %w", err)
	}

	if len(result) < 32 {
		return false, fmt.Errorf("ERC-1155 balanceOf returned insufficient data (len=%d)", len(result))
	}

	var balance *big.Int
	if err := nv.erc1155ABI.UnpackIntoInterface(&balance, "balanceOf", result); err != nil {
		nv.logger.Error("Failed to unpack ERC-1155 balanceOf result", zap.Error(err))
		return false, fmt.Errorf("failed to unpack ERC-1155 balanceOf result: %w", err)
	}

	hasNFT := balance.Cmp(big.NewInt(0)) > 0
	nv.logger.Debug("ERC-1155 ownership verified",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.Bool("has_nft", hasNFT))
	return hasNFT, nil
}

// GetERC1155Balance gets the ERC-1155 token balance for an address and token ID.
func (nv *NFTVerifier) GetERC1155Balance(ctx context.Context, contractAddress, ownerAddress, tokenID string) (*big.Int, error) {
	nv.logger.Debug("Getting ERC-1155 balance",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress),
		zap.String("token_id", tokenID))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return nil, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	data, err := nv.erc1155ABI.Pack("balanceOf", owner, tokenIDInt)
	if err != nil {
		return nil, fmt.Errorf("failed to pack ERC-1155 balanceOf call: %w", err)
	}

	result, err := nv.callContract(ctx, contract, data)
	if err != nil {
		return nil, fmt.Errorf("failed to call ERC-1155 balanceOf: %w", err)
	}

	if len(result) < 32 {
		return nil, fmt.Errorf("ERC-1155 balanceOf returned insufficient data (len=%d)", len(result))
	}

	var balance *big.Int
	if err := nv.erc1155ABI.UnpackIntoInterface(&balance, "balanceOf", result); err != nil {
		return nil, fmt.Errorf("failed to unpack ERC-1155 balanceOf result: %w", err)
	}

	nv.logger.Debug("ERC-1155 balance retrieved",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress),
		zap.String("balance", balance.String()))
	return balance, nil
}

func (nv *NFTVerifier) detectTokenStandardCached(ctx context.Context, contractAddress string) TokenStandard {
	if cached, ok := nv.standardCache.Load(contractAddress); ok {
		entry := cached.(*tokenStandardEntry)
		if time.Since(entry.cachedAt) < tokenStandardCacheTTL {
			return entry.standard
		}
	}
	standard := DetectTokenStandard(ctx, nv.client, contractAddress, nv.logger)
	if standard != TokenStandardUnknown {
		nv.standardCache.Store(contractAddress, &tokenStandardEntry{
			standard: standard,
			cachedAt: time.Now(),
		})
	}
	return standard
}

// VerifyNFTOwnershipAutoDetect detects the token standard and routes to the
// correct verification method. For ERC-721 it uses ownerOf; for ERC-1155 it
// uses balanceOf(address,uint256). Falls back to ERC-721 on detection failure.
func (nv *NFTVerifier) VerifyNFTOwnershipAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	standard := nv.detectTokenStandardCached(ctx, contractAddress)
	switch standard {
	case TokenStandardERC1155:
		return nv.VerifyERC1155Ownership(ctx, contractAddress, tokenID, ownerAddress)
	default:
		return nv.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	}
}

// VerifyNFTCollectionAutoDetect detects the token standard and routes to the
// correct collection-level verification. For ERC-721 it uses balanceOf(address).
// For ERC-1155, collection-level verification without a specific token ID is
// unreliable (checking balanceOf(address,0) is a heuristic that fails when the
// user holds other token IDs). Callers should specify tokenID for ERC-1155
// contracts; if not provided, this returns an error directing the caller to
// supply one.
func (nv *NFTVerifier) VerifyNFTCollectionAutoDetect(ctx context.Context, contractAddress, ownerAddress string) (bool, error) {
	standard := nv.detectTokenStandardCached(ctx, contractAddress)
	switch standard {
	case TokenStandardERC1155:
		return false, fmt.Errorf("ERC-1155 collection verification requires a specific tokenID; provide token_id parameter")
	default:
		return nv.VerifyNFTCollection(ctx, contractAddress, ownerAddress)
	}
}

// GetNFTBalanceAutoDetect detects the token standard and routes to the correct
// balance check. For ERC-721 it uses balanceOf(address); for ERC-1155 it
// requires a tokenID parameter and uses balanceOf(address,uint256).
func (nv *NFTVerifier) GetNFTBalanceAutoDetect(ctx context.Context, contractAddress, ownerAddress string, tokenID ...string) (*big.Int, error) {
	standard := nv.detectTokenStandardCached(ctx, contractAddress)
	switch standard {
	case TokenStandardERC1155:
		if len(tokenID) == 0 || tokenID[0] == "" {
			return nil, fmt.Errorf("ERC-1155 balance query requires a tokenID parameter")
		}
		return nv.GetERC1155Balance(ctx, contractAddress, ownerAddress, tokenID[0])
	default:
		return nv.GetNFTBalance(ctx, contractAddress, ownerAddress)
	}
}

// callContract executes a contract call with blockTag support for reorg protection.
func (nv *NFTVerifier) callContract(ctx context.Context, contract common.Address, data []byte) ([]byte, error) {
	if nv.blockTag != "" && nv.blockTag != BlockTagLatest {
		if btc, ok := nv.client.(BlockTagCaller); ok {
			return btc.CallContractAtBlock(ctx, ethereum.CallMsg{
				To:   &contract,
				Data: data,
			}, nv.blockTag)
		}
		nv.logger.Warn("Client doesn't support block tags, falling back to latest",
			zap.String("block_tag", string(nv.blockTag)))
	}
	return nv.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
}

// ApprovalInfo contains information about NFT approvals for a token.
type ApprovalInfo struct {
	// ApprovedOperator is the address approved for all operations (isApprovedForAll).
	// Empty string means no operator is approved.
	ApprovedOperator string
	// ApprovedAddress is the address approved for a specific token (getApproved).
	// Empty string means no specific approval.
	ApprovedAddress string
}

// CheckApproval checks whether an NFT has active approvals that could allow
// a third party to transfer it. This is important for TOCTOU protection:
// even if the owner currently holds the NFT, an approved operator could
// transfer it immediately after verification.
//
// For ERC-721, this checks getApproved (per-token) and isApprovedForAll
// against known marketplace operators. For ERC-1155, only isApprovedForAll
// applies (no per-token approval concept). Use CheckApprovalAutoDetect for
// automatic standard detection, or call CheckERC1155Approval directly.
func (nv *NFTVerifier) CheckApproval(ctx context.Context, contractAddress, tokenID, ownerAddress string) (*ApprovalInfo, error) {
	nv.logger.Debug("Checking NFT approvals",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)
	info := &ApprovalInfo{}

	// Check getApproved(tokenId) — returns the approved address for a specific token
	// This is ERC-721 only; ERC-1155 doesn't have per-token approvals.
	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return nil, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	data, err := nv.erc721ABI.Pack("getApproved", tokenIDInt)
	if err != nil {
		// Not all contracts implement getApproved — best-effort
		nv.logger.Debug("Failed to pack getApproved call (contract may not support it)", zap.Error(err))
		return info, nil
	}

	result, err := nv.callContract(ctx, contract, data)
	if err != nil {
		nv.logger.Debug("getApproved call failed (contract may not support it)", zap.Error(err))
		return info, nil
	}

	if len(result) >= 32 {
		var approved common.Address
		if err := nv.erc721ABI.UnpackIntoInterface(&approved, "getApproved", result); err == nil {
			if approved != (common.Address{}) {
				info.ApprovedAddress = approved.Hex()
			}
		}
	}

	// Check isApprovedForAll(owner, operator) against known marketplace operators.
	// Both ERC-721 and ERC-1155 define isApprovedForAll(address,address) with
	// the same selector (0xe985e9c5) and return type (bool), so this check
	// works cross-standard.
	operators := nv.KnownOperators
	if len(operators) == 0 {
		operators = DefaultKnownOperators
	}
	for _, op := range operators {
		if !common.IsHexAddress(op) {
			continue
		}
		operatorAddr := common.HexToAddress(op)
		opData, err := nv.erc721ABI.Pack("isApprovedForAll", owner, operatorAddr)
		if err != nil {
			continue
		}
		opResult, err := nv.callContract(ctx, contract, opData)
		if err != nil {
			continue
		}
		if len(opResult) >= 32 {
			var approved bool
			if err := nv.erc721ABI.UnpackIntoInterface(&approved, "isApprovedForAll", opResult); err == nil && approved {
				info.ApprovedOperator = op
				break
			}
		}
	}

	nv.logger.Debug("NFT approval check completed",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("approved_address", info.ApprovedAddress),
		zap.String("approved_operator", info.ApprovedOperator))

	return info, nil
}

// CheckERC1155Approval checks ERC-1155 isApprovedForAll against known
// marketplace operators. ERC-1155 does not have per-token approvals
// (no getApproved equivalent), so only operator-level checks apply.
func (nv *NFTVerifier) CheckERC1155Approval(ctx context.Context, contractAddress, ownerAddress string) (*ApprovalInfo, error) {
	nv.logger.Debug("Checking ERC-1155 approvals",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)
	info := &ApprovalInfo{}

	operators := nv.KnownOperators
	if len(operators) == 0 {
		operators = DefaultKnownOperators
	}
	for _, op := range operators {
		if !common.IsHexAddress(op) {
			continue
		}
		operatorAddr := common.HexToAddress(op)
		opData, err := nv.erc1155ABI.Pack("isApprovedForAll", owner, operatorAddr)
		if err != nil {
			continue
		}
		opResult, err := nv.callContract(ctx, contract, opData)
		if err != nil {
			continue
		}
		if len(opResult) >= 32 {
			var approved bool
			if err := nv.erc1155ABI.UnpackIntoInterface(&approved, "isApprovedForAll", opResult); err == nil && approved {
				info.ApprovedOperator = op
				break
			}
		}
	}

	nv.logger.Debug("ERC-1155 approval check completed",
		zap.String("contract", contractAddress),
		zap.String("approved_operator", info.ApprovedOperator))
	return info, nil
}

// CheckApprovalAutoDetect detects the token standard and routes to the
// correct approval check. For ERC-721 it uses CheckApproval (getApproved +
// isApprovedForAll); for ERC-1155 it uses CheckERC1155Approval
// (isApprovedForAll only). Falls back to ERC-721 on detection failure.
func (nv *NFTVerifier) CheckApprovalAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (*ApprovalInfo, error) {
	standard := nv.detectTokenStandardCached(ctx, contractAddress)
	switch standard {
	case TokenStandardERC1155:
		return nv.CheckERC1155Approval(ctx, contractAddress, ownerAddress)
	default:
		return nv.CheckApproval(ctx, contractAddress, tokenID, ownerAddress)
	}
}

// TokenStandard represents the NFT token standard
type TokenStandard int

const (
	// TokenStandardUnknown means the standard could not be determined
	TokenStandardUnknown TokenStandard = iota
	// TokenStandardERC721 represents ERC-721 (NFT)
	TokenStandardERC721
	// TokenStandardERC1155 represents ERC-1155 (Multi-token)
	TokenStandardERC1155
)

// ERC165 interface IDs
var (
	// erc165InterfaceID is the ERC-165 interface itself
	erc165InterfaceID = common.HexToHash("0x01ffc9a7")
	// erc721InterfaceID is ERC-721 (0x80ac58cd)
	erc721InterfaceID = common.HexToHash("0x80ac58cd")
	// erc1155InterfaceID is ERC-1155 (0xd9b67a26)
	erc1155InterfaceID = common.HexToHash("0xd9b67a26")
)

// erc165ABI is the minimal ABI for supportsInterface
const erc165ABI = `[{"constant":true,"inputs":[{"name":"interfaceID","type":"bytes4"}],"name":"supportsInterface","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"view","type":"function"}]`

// erc165ParsedABI is the pre-parsed ERC-165 ABI, initialized once at package load.
var erc165ParsedABI abi.ABI

func init() {
	var err error
	erc165ParsedABI, err = abi.JSON(strings.NewReader(erc165ABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse ERC-165 ABI: %v", err))
	}
}

// DetectTokenStandard detects whether a contract is ERC-721 or ERC-1155.
// It first checks ERC-165 supportsInterface, then falls back to probing
// balanceOf selectors if ERC-165 is not supported. If neither probe
// produces a definitive result, TokenStandardUnknown is returned.
//
// The fallback probes use balanceOf at zero address/ID; contracts that
// don't revert on unknown selectors may be misidentified. ERC-165
// compliant contracts always produce accurate results.
func DetectTokenStandard(ctx context.Context, caller EthCaller, contractAddress string, logger *zap.Logger) TokenStandard {
	if !common.IsHexAddress(contractAddress) {
		return TokenStandardUnknown
	}

	contract := common.HexToAddress(contractAddress)

	if supports165, err := callSupportsInterface(ctx, caller, erc165ParsedABI, contract, erc165InterfaceID); err == nil && supports165 {
		if supports1155, err := callSupportsInterface(ctx, caller, erc165ParsedABI, contract, erc1155InterfaceID); err == nil && supports1155 {
			return TokenStandardERC1155
		}
		if supports721, err := callSupportsInterface(ctx, caller, erc165ParsedABI, contract, erc721InterfaceID); err == nil && supports721 {
			return TokenStandardERC721
		}
	}

	// Fallback: probe selectors for contracts that don't implement ERC-165.
	if try1155BalanceOf(ctx, caller, contract) {
		return TokenStandardERC1155
	}
	if try721BalanceOf(ctx, caller, contract) {
		return TokenStandardERC721
	}

	// Neither ERC-165 nor selector probing produced a result.
	return TokenStandardUnknown
}

// callSupportsInterface calls supportsInterface on a contract
func callSupportsInterface(ctx context.Context, caller EthCaller, parsedABI abi.ABI, contract common.Address, interfaceID common.Hash) (bool, error) {
	data, err := parsedABI.Pack("supportsInterface", [4]byte(interfaceID[:4]))
	if err != nil {
		return false, err
	}

	result, err := caller.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
	if err != nil {
		return false, err
	}

	var supported bool
	if err := parsedABI.UnpackIntoInterface(&supported, "supportsInterface", result); err != nil {
		return false, err
	}
	return supported, nil
}

// try1155BalanceOf attempts an ERC-1155 balanceOf(address,uint256) call with zero values.
// Returns true if the call succeeds (contract likely supports ERC-1155).
func try1155BalanceOf(ctx context.Context, caller EthCaller, contract common.Address) bool {
	// ERC-1155 balanceOf(address,uint256) selector: 0x00fdd58e
	// Encode: selector + padded(address) + padded(uint256)
	selector := common.Hex2Bytes("00fdd58e")
	zeroAddr := common.Address{}
	paddedAddr := common.LeftPadBytes(zeroAddr.Bytes(), 32)
	paddedID := common.LeftPadBytes(big.NewInt(0).Bytes(), 32)

	data := make([]byte, 0, 4+32+32)
	data = append(data, selector...)
	data = append(data, paddedAddr...)
	data = append(data, paddedID...)

	_, err := caller.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
	return err == nil
}

// try721BalanceOf attempts an ERC-721 balanceOf(address) call with zero address.
// Returns true if the call succeeds (contract likely supports ERC-721).
func try721BalanceOf(ctx context.Context, caller EthCaller, contract common.Address) bool {
	// ERC-721 balanceOf(address) selector: 0x70a08231
	selector := common.Hex2Bytes("70a08231")
	zeroAddr := common.Address{}
	paddedAddr := common.LeftPadBytes(zeroAddr.Bytes(), 32)

	data := make([]byte, 0, 4+32)
	data = append(data, selector...)
	data = append(data, paddedAddr...)

	_, err := caller.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
	return err == nil
}
