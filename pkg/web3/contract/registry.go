package contract

import (
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

var (
	// Event signatures must match contracts/src/ContentRegistry.sol exactly.
	// abi_sync_test.go fails the build when either side drifts.
	contentRegisteredTopic = crypto.Keccak256Hash([]byte("ContentRegistered(bytes32,string,address,bytes32,string,uint256)"))
	contentStatusTopic     = crypto.Keccak256Hash([]byte("ContentStatusUpdated(bytes32,string,uint8,uint8,uint256)"))
)

type ContentRegistry struct {
	Address string
	ABI     string
	Events  map[string]string
}

func NewContentRegistry(address string) *ContentRegistry {
	return &ContentRegistry{
		Address: address,
		ABI:     ContentRegistryABI,
		Events: map[string]string{
			"ContentRegistered":    contentRegisteredTopic.Hex(),
			"ContentStatusUpdated": contentStatusTopic.Hex(),
		},
	}
}

// ContentRegistryABI mirrors contracts/src/ContentRegistry.sol — the two MUST
// stay in sync (the 2026-10-10 review found them diverged, which made every
// on-chain registration call revert against the deployed contract).
const ContentRegistryABI = `[
  {"inputs":[{"name":"contentId","type":"string"},{"name":"creator","type":"address"},{"name":"contentDigest","type":"bytes32"},{"name":"metadataURI","type":"string"}],"name":"registerContent","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"name":"contentId","type":"string"}],"name":"contentExists","outputs":[{"name":"","type":"bool"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"contentId","type":"string"}],"name":"getContent","outputs":[{"name":"record","type":"tuple","components":[{"name":"contentId","type":"string"},{"name":"creator","type":"address"},{"name":"contentDigest","type":"bytes32"},{"name":"metadataURI","type":"string"},{"name":"status","type":"uint8"},{"name":"createdAt","type":"uint256"},{"name":"updatedAt","type":"uint256"}]}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"getContentCount","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"offset","type":"uint256"},{"name":"limit","type":"uint256"}],"name":"getContentIds","outputs":[{"name":"page","type":"string[]"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"contentId","type":"string"},{"name":"newStatus","type":"uint8"}],"name":"updateContentStatus","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"name":"contentId","type":"string"}],"name":"removeContent","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"contentKey","type":"bytes32"},{"indexed":false,"name":"contentId","type":"string"},{"indexed":true,"name":"creator","type":"address"},{"indexed":false,"name":"contentDigest","type":"bytes32"},{"indexed":false,"name":"metadataURI","type":"string"},{"indexed":false,"name":"timestamp","type":"uint256"}],"name":"ContentRegistered","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"contentKey","type":"bytes32"},{"indexed":false,"name":"contentId","type":"string"},{"indexed":false,"name":"oldStatus","type":"uint8"},{"indexed":false,"name":"newStatus","type":"uint8"},{"indexed":false,"name":"timestamp","type":"uint256"}],"name":"ContentStatusUpdated","type":"event"},
  {"inputs":[{"name":"contentId","type":"string"}],"name":"ContentAlreadyExists","type":"error"},
  {"inputs":[{"name":"contentId","type":"string"}],"name":"ContentNotFound","type":"error"},
  {"inputs":[{"name":"caller","type":"address"},{"name":"creator","type":"address"}],"name":"UnauthorizedCaller","type":"error"},
  {"inputs":[{"name":"from","type":"uint8"},{"name":"to","type":"uint8"}],"name":"IllegalStateTransition","type":"error"},
  {"inputs":[],"name":"InvalidCreator","type":"error"},
  {"inputs":[],"name":"InvalidContentId","type":"error"},
  {"inputs":[],"name":"InvalidContentDigest","type":"error"},
  {"inputs":[],"name":"MetadataURITooLong","type":"error"},
  {"inputs":[],"name":"InvalidPagination","type":"error"},
  {"inputs":[],"name":"RenounceDisabled","type":"error"}
]`

const ContentRegistryBytecode = "0x"

type NFTContract struct {
	Address string
	ABI     string
	Events  map[string]string
}

var (
	transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	approvalTopic = crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
)

func NewNFTContract(address string) *NFTContract {
	return &NFTContract{
		Address: address,
		ABI:     ERC721ABI,
		Events: map[string]string{
			"Transfer": transferTopic.Hex(),
			"Approval": approvalTopic.Hex(),
		},
	}
}

const ERC721ABI = `[
  {
    "inputs": [
      {"name": "to", "type": "address"}
    ],
    "name": "mint",
    "outputs": [{"name": "", "type": "uint256"}],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "tokenId", "type": "uint256"}
    ],
    "name": "ownerOf",
    "outputs": [{"name": "", "type": "address"}],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "owner", "type": "address"}
    ],
    "name": "balanceOf",
    "outputs": [{"name": "", "type": "uint256"}],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "anonymous": false,
    "inputs": [
      {"indexed": true, "name": "from", "type": "address"},
      {"indexed": true, "name": "to", "type": "address"},
      {"indexed": true, "name": "tokenId", "type": "uint256"}
    ],
    "name": "Transfer",
    "type": "event"
  }
]`

const BalanceOfABIJSON = `[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`

type SmartContractRegistry struct {
	mu        sync.RWMutex
	contracts map[string]*SmartContractInfo
	byAddr    map[string]*SmartContractInfo
	logger    *zap.Logger
}

type SmartContractInfo struct {
	Name       string
	Address    string
	ChainID    int64
	ABI        string
	Bytecode   string
	DeployedAt int64
	Verified   bool
	SourceCode string
}

func NewSmartContractRegistry(logger *zap.Logger) *SmartContractRegistry {
	return &SmartContractRegistry{
		contracts: make(map[string]*SmartContractInfo),
		byAddr:    make(map[string]*SmartContractInfo),
		logger:    logger,
	}
}

func (scr *SmartContractRegistry) RegisterContract(info *SmartContractInfo) {
	scr.mu.Lock()
	defer scr.mu.Unlock()
	scr.logger.Info("Registering contract",
		zap.String("name", info.Name),
		zap.String("address", info.Address),
		zap.Int64("chain_id", info.ChainID))
	scr.contracts[info.Name] = info
	scr.byAddr[info.Address] = info
}

func (scr *SmartContractRegistry) GetContract(name string) *SmartContractInfo {
	scr.mu.RLock()
	defer scr.mu.RUnlock()
	return scr.contracts[name]
}

func (scr *SmartContractRegistry) GetContractByAddress(address string) *SmartContractInfo {
	scr.mu.RLock()
	defer scr.mu.RUnlock()
	return scr.byAddr[address]
}

func (scr *SmartContractRegistry) GetAllContracts() map[string]*SmartContractInfo {
	scr.mu.RLock()
	defer scr.mu.RUnlock()
	out := make(map[string]*SmartContractInfo, len(scr.contracts))
	for k, v := range scr.contracts {
		out[k] = v
	}
	return out
}

func (scr *SmartContractRegistry) GetContractsByChain(chainID int64) []*SmartContractInfo {
	scr.mu.RLock()
	defer scr.mu.RUnlock()
	result := make([]*SmartContractInfo, 0)
	for _, contract := range scr.contracts {
		if contract.ChainID == chainID {
			result = append(result, contract)
		}
	}
	return result
}
