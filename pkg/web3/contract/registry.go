package contract

import (
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

var (
	contentRegisteredTopic = crypto.Keccak256Hash([]byte("ContentRegistered(bytes32,address,uint256)"))
	contentVerifiedTopic   = crypto.Keccak256Hash([]byte("ContentVerified(bytes32,bool)"))
	contentDeletedTopic    = crypto.Keccak256Hash([]byte("ContentDeleted(bytes32,address)"))
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
			"ContentRegistered": contentRegisteredTopic.Hex(),
			"ContentVerified":   contentVerifiedTopic.Hex(),
			"ContentDeleted":    contentDeletedTopic.Hex(),
		},
	}
}

const ContentRegistryABI = `[
  {
    "inputs": [
      {"name": "contentHash", "type": "bytes32"},
      {"name": "metadata", "type": "string"}
    ],
    "name": "registerContent",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "contentHash", "type": "bytes32"}
    ],
    "name": "verifyContent",
    "outputs": [{"name": "", "type": "bool"}],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "contentHash", "type": "bytes32"}
    ],
    "name": "getContentInfo",
    "outputs": [
      {"name": "owner", "type": "address"},
      {"name": "timestamp", "type": "uint256"},
      {"name": "metadata", "type": "string"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "anonymous": false,
    "inputs": [
      {"indexed": true, "name": "contentHash", "type": "bytes32"},
      {"indexed": true, "name": "owner", "type": "address"},
      {"indexed": false, "name": "timestamp", "type": "uint256"}
    ],
    "name": "ContentRegistered",
    "type": "event"
  }
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
      {"name": "to", "type": "address"},
      {"name": "tokenId", "type": "uint256"}
    ],
    "name": "mint",
    "outputs": [],
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
