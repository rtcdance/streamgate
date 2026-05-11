package web3

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// EthCaller abstracts the Ethereum contract call interface.
// *ethclient.Client satisfies this interface implicitly.
//go:generate mockgen -destination=mocks/mock_eth_caller.go -package=mocks streamgate/pkg/web3 EthCaller
type EthCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

// NFTVerifier handles NFT verification
type NFTVerifier struct {
	client         EthCaller
	logger         *zap.Logger
	erc721ABI      abi.ABI // pre-parsed at construction
	erc721MetaABI  abi.ABI // pre-parsed: name, symbol, tokenURI, ownerOf
	KnownOperators []string // known marketplace/operator contracts to check isApprovedForAll
	blockTag       BlockTag // block tag for reading state (default: BlockTagLatest)
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

// NewNFTVerifier creates a new NFT verifier
func NewNFTVerifier(client EthCaller, logger *zap.Logger) *NFTVerifier {
	return &NFTVerifier{
		client:        client,
		logger:        logger,
		erc721ABI:     mustParseABI("ERC-721", erc721ABIJSON),
		erc721MetaABI: mustParseABI("ERC-721-meta", erc721MetaABIJSON),
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

	// Execute call — use blockTag for reorg protection if configured
	var result []byte
	if nv.blockTag != "" && nv.blockTag != BlockTagLatest {
		if btc, ok := nv.client.(BlockTagCaller); ok {
			result, err = btc.CallContractAtBlock(ctx, ethereum.CallMsg{
				To:   &contract,
				Data: data,
			}, nv.blockTag)
		} else {
			// Client doesn't support block tags, fall back to latest
			nv.logger.Warn("Client doesn't support block tags, falling back to latest",
				zap.String("block_tag", string(nv.blockTag)))
			result, err = nv.client.CallContract(ctx, ethereum.CallMsg{
				To:   &contract,
				Data: data,
			}, nil)
		}
	} else {
		result, err = nv.client.CallContract(ctx, ethereum.CallMsg{
			To:   &contract,
			Data: data,
		}, nil)
	}

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

	// Execute call
	result, err := nv.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)

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

	// Best-effort: name
	if data, err := nv.erc721MetaABI.Pack("name"); err == nil {
		if result, err := nv.client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil); err == nil {
			if len(result) >= 32 {
				var name string
				if err := nv.erc721MetaABI.UnpackIntoInterface(&name, "name", result); err == nil {
					nftInfo.Name = name
				}
			}
		}
	}

	// Best-effort: symbol
	if data, err := nv.erc721MetaABI.Pack("symbol"); err == nil {
		if result, err := nv.client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil); err == nil {
			if len(result) >= 32 {
				var symbol string
				if err := nv.erc721MetaABI.UnpackIntoInterface(&symbol, "symbol", result); err == nil {
					nftInfo.Symbol = symbol
				}
			}
		}
	}

	// Best-effort: tokenURI
	if data, err := nv.erc721MetaABI.Pack("tokenURI", tokenIDInt); err == nil {
		if result, err := nv.client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil); err == nil {
			if len(result) >= 32 {
				var tokenURI string
				if err := nv.erc721MetaABI.UnpackIntoInterface(&tokenURI, "tokenURI", result); err == nil {
					nftInfo.URI = tokenURI
				}
			}
		}
	}

	// Best-effort: ownerOf
	if data, err := nv.erc721MetaABI.Pack("ownerOf", tokenIDInt); err == nil {
		if result, err := nv.client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil); err == nil {
			if len(result) >= 32 {
				var owner common.Address
				if err := nv.erc721MetaABI.UnpackIntoInterface(&owner, "ownerOf", result); err == nil {
					nftInfo.Owner = owner.Hex()
				}
			}
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

	// Get balance
	balance, err := nv.GetNFTBalance(ctx, contractAddress, ownerAddress)
	if err != nil {
		return false, err
	}

	// Check if balance > 0
	hasNFT := balance.Cmp(big.NewInt(0)) > 0
	nv.logger.Debug("NFT collection ownership verified",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress),
		zap.Bool("has_nft", hasNFT))

	return hasNFT, nil
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
func (nv *NFTVerifier) CheckApproval(ctx context.Context, contractAddress, tokenID, ownerAddress string) (*ApprovalInfo, error) {
	nv.logger.Debug("Checking NFT approvals",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)
	info := &ApprovalInfo{}

	// Check getApproved(tokenId) — returns the approved address for a specific token
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

	result, err := nv.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, nil)
	if err != nil {
		// Some contracts revert on getApproved for non-existent tokens
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
		opResult, err := nv.client.CallContract(ctx, ethereum.CallMsg{
			To:   &contract,
			Data: opData,
		}, nil)
		if err != nil {
			// Contract may not support isApprovedForAll — skip
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

// DetectTokenStandard detects whether a contract is ERC-721 or ERC-1155.
// It first checks ERC-165 supportsInterface, then falls back to probing
// balanceOf selectors if ERC-165 is not supported.
func DetectTokenStandard(ctx context.Context, caller EthCaller, contractAddress string, logger *zap.Logger) TokenStandard {
	if !common.IsHexAddress(contractAddress) {
		return TokenStandardUnknown
	}

	contract := common.HexToAddress(contractAddress)

	// Try ERC-165 supportsInterface
	parsedABI, err := abi.JSON(strings.NewReader(erc165ABI))
	if err != nil {
		logger.Debug("Failed to parse ERC-165 ABI, defaulting to ERC-721", zap.Error(err))
		return TokenStandardERC721
	}

	// Check if contract supports ERC-165 itself
	if supports165, err := callSupportsInterface(ctx, caller, parsedABI, contract, erc165InterfaceID); err == nil && supports165 {
		// Now check specific interfaces
		if supports1155, err := callSupportsInterface(ctx, caller, parsedABI, contract, erc1155InterfaceID); err == nil && supports1155 {
			return TokenStandardERC1155
		}
		if supports721, err := callSupportsInterface(ctx, caller, parsedABI, contract, erc721InterfaceID); err == nil && supports721 {
			return TokenStandardERC721
		}
	}

	// Fallback: try ERC-1155 balanceOf(address,uint256) selector (0x00fdd58e).
	// If the contract responds without revert, it's likely ERC-1155.
	// ERC-721 balanceOf(address) selector is 0x70a08231.
	if try1155BalanceOf(ctx, caller, contract) {
		return TokenStandardERC1155
	}

	// Default to ERC-721 — it's the more common standard
	return TokenStandardERC721
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
