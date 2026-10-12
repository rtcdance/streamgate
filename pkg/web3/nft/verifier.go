package nft

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
	"github.com/rtcdance/streamgate/pkg/web3/internal/abiutil"
	"go.uber.org/zap"
)

type EthCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

type tokenStandardEntry struct {
	standard TokenStandard
	cachedAt time.Time
}

const tokenStandardCacheTTL = 1 * time.Hour

type NFTVerifier struct {
	client         EthCaller
	logger         *zap.Logger
	erc721ABI      abi.ABI
	erc721MetaABI  abi.ABI
	erc1155ABI     abi.ABI
	KnownOperators []string
	blockTag       BlockTag
	// blockNumber, when set, pins every decision query to one exact block. It
	// takes precedence over blockTag: a tag is resolved by the node on each
	// call, so a new block arriving mid-read would let the ERC-165 probe and the
	// balance query answer from different blocks.
	blockNumber   *big.Int
	standardCache sync.Map
}

type BlockTagCaller interface {
	CallContractAtBlock(ctx context.Context, msg ethereum.CallMsg, blockTag BlockTag) ([]byte, error)
}

// BlockNumberCaller is implemented by clients that can pin a call to an exact
// block number.
type BlockNumberCaller interface {
	CallContractAtBlockNumber(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

var DefaultKnownOperators = []string{
	"0x000000a95b0a88b3E564F5e5101CE5F02393a849",
	"0x2a5c4E3CE3dBD1686e6c1A57a1cE6253a2B96480",
	"0x59728544B08AB483533076417FbBB2fD0B17ce3a",
	"0xF5b12ABAa25696301A4b991c0D9f6B4A5c297383",
	"0xF849de01B2133aC8d9a1f89B175A02ad0a2f4394",
}

const erc721ABIJSON = `[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"operator","type":"address"}],"name":"isApprovedForAll","outputs":[{"name":"","type":"bool"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"getApproved","outputs":[{"name":"","type":"address"}],"type":"function"}]`

const erc721MetaABIJSON = `[{"constant":true,"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`

const erc1155ABIJSON = `[{"constant":true,"inputs":[{"name":"account","type":"address"},{"name":"id","type":"uint256"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},{"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"operator","type":"address"}],"name":"isApprovedForAll","outputs":[{"name":"","type":"bool"}],"type":"function"}]`

var (
	preparsedERC721ABI     = abiutil.MustParseABI("ERC-721", erc721ABIJSON)
	preparsedERC721MetaABI = abiutil.MustParseABI("ERC-721-meta", erc721MetaABIJSON)
	preparsedERC1155ABI    = abiutil.MustParseABI("ERC-1155", erc1155ABIJSON)
)

func NewNFTVerifier(client EthCaller, logger *zap.Logger) *NFTVerifier {
	return &NFTVerifier{
		client:         client,
		logger:         logger,
		erc721ABI:      preparsedERC721ABI,
		erc721MetaABI:  preparsedERC721MetaABI,
		erc1155ABI:     preparsedERC1155ABI,
		blockTag:       BlockTagSafe,
		KnownOperators: DefaultKnownOperators,
	}
}

func (nv *NFTVerifier) WithBlockTag(tag BlockTag) *NFTVerifier {
	nv.blockTag = tag
	return nv
}

// WithBlockNumber pins every decision query to an exact block. Passing nil
// clears the pin and returns the verifier to tag-based reads.
func (nv *NFTVerifier) WithBlockNumber(blockNumber *big.Int) *NFTVerifier {
	if blockNumber == nil {
		nv.blockNumber = nil
		return nv
	}
	nv.blockNumber = new(big.Int).Set(blockNumber)
	return nv
}

func (nv *NFTVerifier) VerifyNFTOwnership(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	nv.logger.Debug("Verifying NFT ownership",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)

	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return false, fmt.Errorf("invalid token ID: %s", tokenID)
	}

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

	var tokenOwner common.Address
	err = nv.erc721ABI.UnpackIntoInterface(&tokenOwner, "ownerOf", result)
	if err != nil {
		nv.logger.Error("Failed to unpack ownerOf result", zap.Error(err))
		return false, fmt.Errorf("failed to unpack ownerOf result: %w", err)
	}

	isOwner := tokenOwner == owner
	nv.logger.Debug("NFT ownership verified",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.Bool("is_owner", isOwner))

	return isOwner, nil
}

func (nv *NFTVerifier) GetNFTBalance(ctx context.Context, contractAddress, ownerAddress string) (*big.Int, error) {
	nv.logger.Debug("Getting NFT balance",
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)

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

type NFTInfo struct {
	ContractAddress string
	TokenID         string
	Owner           string
	Name            string
	Symbol          string
	URI             string
	Warnings        []string
}

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
		if entry, ok := cached.(*tokenStandardEntry); ok && time.Since(entry.cachedAt) < tokenStandardCacheTTL {
			return entry.standard
		}
	}
	// When a block is pinned, the probe MUST go out at that block. Calling the
	// unpinned probe first and discarding its answer would still send a
	// latest-block eth_call to the node - a read the pin exists to prevent.
	var standard TokenStandard
	if nv.blockNumber != nil {
		if bnc, ok := nv.client.(BlockNumberCaller); ok {
			standard = DetectTokenStandardAtBlock(ctx, bnc, contractAddress, nv.blockNumber, nv.logger)
		} else {
			// Cannot pin the ERC-165 probe: do not fall back to a latest-block
			// probe, which could disagree with the pinned balance query.
			standard = TokenStandardUnknown
		}
	} else {
		standard = DetectTokenStandard(ctx, nv.client, contractAddress, nv.logger)
	}
	if standard != TokenStandardUnknown {
		nv.standardCache.Store(contractAddress, &tokenStandardEntry{
			standard: standard,
			cachedAt: time.Now(),
		})
	}
	return standard
}

func (nv *NFTVerifier) VerifyNFTOwnershipAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	standard := nv.detectTokenStandardCached(ctx, contractAddress)
	switch standard {
	case TokenStandardERC1155:
		return nv.VerifyERC1155Ownership(ctx, contractAddress, tokenID, ownerAddress)
	default:
		return nv.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	}
}

func (nv *NFTVerifier) VerifyNFTCollectionAutoDetect(ctx context.Context, contractAddress, ownerAddress string) (bool, error) {
	standard := nv.detectTokenStandardCached(ctx, contractAddress)
	switch standard {
	case TokenStandardERC1155:
		return false, fmt.Errorf("ERC-1155 collection verification requires a specific tokenID; provide token_id parameter")
	default:
		return nv.VerifyNFTCollection(ctx, contractAddress, ownerAddress)
	}
}

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

func (nv *NFTVerifier) callContract(ctx context.Context, contract common.Address, data []byte) ([]byte, error) {
	// A pinned block wins over the tag. If the client cannot pin, refuse: falling
	// back to a tag (or to latest) would break the guarantee the pin exists for.
	if nv.blockNumber != nil {
		bnc, ok := nv.client.(BlockNumberCaller)
		if !ok {
			return nil, fmt.Errorf("client cannot pin calls to block %s; refusing to read at a tag or latest", nv.blockNumber)
		}
		return bnc.CallContractAtBlockNumber(ctx, ethereum.CallMsg{
			To:   &contract,
			Data: data,
		}, nv.blockNumber)
	}
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

type ApprovalInfo struct {
	ApprovedOperator string
	ApprovedAddress  string
}

func (nv *NFTVerifier) CheckApproval(ctx context.Context, contractAddress, tokenID, ownerAddress string) (*ApprovalInfo, error) {
	nv.logger.Debug("Checking NFT approvals",
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	contract := common.HexToAddress(contractAddress)
	owner := common.HexToAddress(ownerAddress)
	info := &ApprovalInfo{}

	tokenIDInt := new(big.Int)
	if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
		return nil, fmt.Errorf("invalid token ID: %s", tokenID)
	}

	data, err := nv.erc721ABI.Pack("getApproved", tokenIDInt)
	if err != nil {
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

func (nv *NFTVerifier) CheckApprovalAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (*ApprovalInfo, error) {
	standard := nv.detectTokenStandardCached(ctx, contractAddress)
	switch standard {
	case TokenStandardERC1155:
		return nv.CheckERC1155Approval(ctx, contractAddress, ownerAddress)
	default:
		return nv.CheckApproval(ctx, contractAddress, tokenID, ownerAddress)
	}
}

type TokenStandard int

const (
	TokenStandardUnknown TokenStandard = iota
	TokenStandardERC721
	TokenStandardERC1155
)

var (
	erc165InterfaceID  = [4]byte{0x01, 0xff, 0xc9, 0xa7}
	erc721InterfaceID  = [4]byte{0x80, 0xac, 0x58, 0xcd}
	erc1155InterfaceID = [4]byte{0xd9, 0xb6, 0x7a, 0x26}
)

const erc165ABI = `[{"constant":true,"inputs":[{"name":"interfaceID","type":"bytes4"}],"name":"supportsInterface","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"view","type":"function"}]`

var erc165ParsedABI abi.ABI

func init() {
	var err error
	erc165ParsedABI, err = abi.JSON(strings.NewReader(erc165ABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse ERC-165 ABI: %v", err))
	}
}

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

	// ERC-165 is the only accepted source of truth. A balanceOf probe is NOT a
	// fallback: ERC-20 exposes balanceOf(address) exactly like ERC-721, so
	// probing would classify a fungible token as an NFT and let it satisfy NFT
	// gating. Unknown / non-ERC-165 contracts are rejected instead.
	return TokenStandardUnknown
}

func callSupportsInterface(ctx context.Context, caller EthCaller, parsedABI abi.ABI, contract common.Address, interfaceID [4]byte) (bool, error) {
	data, err := parsedABI.Pack("supportsInterface", interfaceID)
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

// DetectTokenStandardAtBlock is DetectTokenStandard pinned to one exact block,
// so the ERC-165 probe reads the same state as the balance query that follows.
// It has the same ERC-165-only rule: a balanceOf probe is never a fallback.
func DetectTokenStandardAtBlock(ctx context.Context, caller BlockNumberCaller, contractAddress string, blockNumber *big.Int, logger *zap.Logger) TokenStandard {
	if !common.IsHexAddress(contractAddress) {
		return TokenStandardUnknown
	}
	contract := common.HexToAddress(contractAddress)

	if supports165, err := callSupportsInterfaceAtBlock(ctx, caller, erc165ParsedABI, contract, erc165InterfaceID, blockNumber); err == nil && supports165 {
		if supports1155, err := callSupportsInterfaceAtBlock(ctx, caller, erc165ParsedABI, contract, erc1155InterfaceID, blockNumber); err == nil && supports1155 {
			return TokenStandardERC1155
		}
		if supports721, err := callSupportsInterfaceAtBlock(ctx, caller, erc165ParsedABI, contract, erc721InterfaceID, blockNumber); err == nil && supports721 {
			return TokenStandardERC721
		}
	}
	return TokenStandardUnknown
}

func callSupportsInterfaceAtBlock(ctx context.Context, caller BlockNumberCaller, parsedABI abi.ABI, contract common.Address, interfaceID [4]byte, blockNumber *big.Int) (bool, error) {
	data, err := parsedABI.Pack("supportsInterface", interfaceID)
	if err != nil {
		return false, err
	}

	result, err := caller.CallContractAtBlockNumber(ctx, ethereum.CallMsg{
		To:   &contract,
		Data: data,
	}, blockNumber)
	if err != nil {
		return false, err
	}

	var supported bool
	if err := parsedABI.UnpackIntoInterface(&supported, "supportsInterface", result); err != nil {
		return false, err
	}
	return supported, nil
}

// GetNFTQuantityAutoDetect returns the holder's quantity for the gating
// subject, using one consistent semantic across standards:
//
//   - tokenID == "" : collection balance (ERC-721 balanceOf). ERC-1155 has no
//     collection-wide total, so it errors instead of inventing one.
//   - tokenID != "" : ERC-721 yields 0 or 1 (ownerOf match); ERC-1155 yields
//     the on-chain uint256 quantity for that id.
//
// Unknown / non-NFT contracts error. A nil result or an error must never be
// treated as "holds enough" by callers.
func (nv *NFTVerifier) GetNFTQuantityAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (*big.Int, error) {
	if !common.IsHexAddress(contractAddress) {
		return nil, fmt.Errorf("invalid contract address: %s", contractAddress)
	}
	if !common.IsHexAddress(ownerAddress) {
		return nil, fmt.Errorf("invalid owner address: %s", ownerAddress)
	}

	switch nv.detectTokenStandardCached(ctx, contractAddress) {
	case TokenStandardERC1155:
		if tokenID == "" {
			return nil, fmt.Errorf("ERC-1155 has no collection-wide balance; a tokenID is required")
		}
		return nv.GetERC1155Balance(ctx, contractAddress, ownerAddress, tokenID)
	case TokenStandardERC721:
		if tokenID == "" {
			return nv.GetNFTBalance(ctx, contractAddress, ownerAddress)
		}
		owned, err := nv.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
		if err != nil {
			return nil, err
		}
		if owned {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	default:
		return nil, fmt.Errorf("contract %s is not a recognised ERC-721/ERC-1155 contract", contractAddress)
	}
}

type BlockTag string

const (
	BlockTagLatest    BlockTag = "latest"
	BlockTagSafe      BlockTag = "safe"
	BlockTagFinalized BlockTag = "finalized"
)
