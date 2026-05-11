package web3

import (
	"fmt"
	"sort"

	"go.uber.org/zap"
)

// ChainConfig represents a blockchain configuration
type ChainConfig struct {
	ID        int64
	Name      string
	RPC       string
	RPCs      []string
	Explorer  string
	Currency  string
	IsTestnet bool
}

// SupportedChains defines supported blockchains
var SupportedChains = map[int64]*ChainConfig{
	// Ethereum
	1: {
		ID:        1,
		Name:      "Ethereum",
		RPC:       "https://eth.llamarpc.com",
		RPCs:      []string{"https://eth.llamarpc.com", "https://ethereum-rpc.publicnode.com"},
		Explorer:  "https://etherscan.io",
		Currency:  "ETH",
		IsTestnet: false,
	},
	11155111: {
		ID:        11155111,
		Name:      "Ethereum Sepolia",
		RPC:       "https://rpc.sepolia.org",
		RPCs:      []string{"https://rpc.sepolia.org", "https://ethereum-sepolia-rpc.publicnode.com"},
		Explorer:  "https://sepolia.etherscan.io",
		Currency:  "ETH",
		IsTestnet: true,
	},

	// Polygon
	137: {
		ID:        137,
		Name:      "Polygon",
		RPC:       "https://polygon-rpc.com",
		RPCs:      []string{"https://polygon-rpc.com", "https://polygon-bor-rpc.publicnode.com"},
		Explorer:  "https://polygonscan.com",
		Currency:  "MATIC",
		IsTestnet: false,
	},
	80002: {
		ID:        80002,
		Name:      "Polygon Amoy",
		RPC:       "https://rpc-amoy.polygon.technology",
		RPCs:      []string{"https://rpc-amoy.polygon.technology"},
		Explorer:  "https://amoy.polygonscan.com",
		Currency:  "MATIC",
		IsTestnet: true,
	},

	// Binance Smart Chain
	56: {
		ID:        56,
		Name:      "Binance Smart Chain",
		RPC:       "https://bsc-dataseed1.binance.org:8545",
		RPCs:      []string{"https://bsc-dataseed1.binance.org:8545", "https://bsc-rpc.publicnode.com"},
		Explorer:  "https://bscscan.com",
		Currency:  "BNB",
		IsTestnet: false,
	},
	97: {
		ID:        97,
		Name:      "BSC Testnet",
		RPC:       "https://data-seed-prebsc-1-b.binance.org:8545",
		RPCs:      []string{"https://data-seed-prebsc-1-b.binance.org:8545"},
		Explorer:  "https://testnet.bscscan.com",
		Currency:  "BNB",
		IsTestnet: true,
	},

	// Arbitrum
	42161: {
		ID:        42161,
		Name:      "Arbitrum One",
		RPC:       "https://arb1.arbitrum.io/rpc",
		RPCs:      []string{"https://arb1.arbitrum.io/rpc", "https://arbitrum-one-rpc.publicnode.com"},
		Explorer:  "https://arbiscan.io",
		Currency:  "ETH",
		IsTestnet: false,
	},
	421614: {
		ID:        421614,
		Name:      "Arbitrum Sepolia",
		RPC:       "https://sepolia-rollup.arbitrum.io/rpc",
		RPCs:      []string{"https://sepolia-rollup.arbitrum.io/rpc"},
		Explorer:  "https://sepolia.arbiscan.io",
		Currency:  "ETH",
		IsTestnet: true,
	},

	// Optimism
	10: {
		ID:        10,
		Name:      "Optimism",
		RPC:       "https://mainnet.optimism.io",
		RPCs:      []string{"https://mainnet.optimism.io", "https://optimism-rpc.publicnode.com"},
		Explorer:  "https://optimistic.etherscan.io",
		Currency:  "ETH",
		IsTestnet: false,
	},
	11155420: {
		ID:        11155420,
		Name:      "Optimism Sepolia",
		RPC:       "https://sepolia.optimism.io",
		RPCs:      []string{"https://sepolia.optimism.io"},
		Explorer:  "https://sepolia-optimistic.etherscan.io",
		Currency:  "ETH",
		IsTestnet: true,
	},

	// Solana
	-1: {
		ID:        -1,
		Name:      "Solana Mainnet",
		RPC:       "https://api.mainnet-beta.solana.com",
		RPCs:      []string{"https://api.mainnet-beta.solana.com"},
		Explorer:  "https://solscan.io",
		Currency:  "SOL",
		IsTestnet: false,
	},
	-2: {
		ID:        -2,
		Name:      "Solana Devnet",
		RPC:       "https://api.devnet.solana.com",
		RPCs:      []string{"https://api.devnet.solana.com"},
		Explorer:  "https://devnet.solscan.io",
		Currency:  "SOL",
		IsTestnet: true,
	},
}

// MultiChainManager manages multiple blockchain connections
type MultiChainManager struct {
	clients       map[int64]*ChainClient
	solanaClients map[int64]*SolanaVerifier
	rateLimiter   *RPCRateLimiter
	logger        *zap.Logger
}

// NewMultiChainManager creates a new multi-chain manager
func NewMultiChainManager(logger *zap.Logger) *MultiChainManager {
	return &MultiChainManager{
		clients:       make(map[int64]*ChainClient),
		solanaClients: make(map[int64]*SolanaVerifier),
		logger:        logger,
	}
}

// AddChain adds a blockchain connection
func (mcm *MultiChainManager) AddChain(chainID int64) error {
	mcm.logger.Info("Adding chain",
		zap.Int64("chain_id", chainID))

	// Get chain config
	config, exists := SupportedChains[chainID]
	if !exists {
		mcm.logger.Error("Chain not supported",
			zap.Int64("chain_id", chainID))
		return fmt.Errorf("chain not supported: %d", chainID)
	}

	// Solana chains use SolanaVerifier
	if chainID < 0 {
		verifier := NewSolanaVerifier(mcm.logger, config.RPC)
		mcm.solanaClients[chainID] = verifier
		mcm.logger.Info("Solana chain added",
			zap.Int64("chain_id", chainID),
			zap.String("name", config.Name))
		return nil
	}

	// Create EVM client
	rpcURLs := config.RPCs
	if len(rpcURLs) == 0 && config.RPC != "" {
		rpcURLs = []string{config.RPC}
	}
	client, err := NewChainClientWithFallback(rpcURLs, chainID, mcm.logger)
	if err != nil {
		mcm.logger.Error("Failed to create chain client",
			zap.Int64("chain_id", chainID),
			zap.Error(err))
		return err
	}

	mcm.clients[chainID] = client
	mcm.logger.Info("Chain added",
		zap.Int64("chain_id", chainID),
		zap.String("name", config.Name))

	return nil
}

// AddChainWithClient adds a chain with an existing client.
func (mcm *MultiChainManager) AddChainWithClient(chainID int64, client *ChainClient) {
	mcm.clients[chainID] = client
}

// RemoveChain removes a blockchain connection
func (mcm *MultiChainManager) RemoveChain(chainID int64) {
	mcm.logger.Info("Removing chain",
		zap.Int64("chain_id", chainID))

	if client, exists := mcm.clients[chainID]; exists {
		client.Close()
		delete(mcm.clients, chainID)
		mcm.logger.Info("Chain removed",
			zap.Int64("chain_id", chainID))
	}
	if _, exists := mcm.solanaClients[chainID]; exists {
		delete(mcm.solanaClients, chainID)
		mcm.logger.Info("Solana chain removed",
			zap.Int64("chain_id", chainID))
	}
}

// GetClient gets a chain client
func (mcm *MultiChainManager) GetClient(chainID int64) (*ChainClient, error) {
	client, exists := mcm.clients[chainID]
	if !exists {
		// Check if it's a Solana chain
		if _, solExists := mcm.solanaClients[chainID]; solExists {
			return nil, fmt.Errorf("EVM chain client not found: %d is a Solana chain", chainID)
		}
		mcm.logger.Error("EVM chain client not found",
			zap.Int64("chain_id", chainID))
		return nil, fmt.Errorf("EVM chain client not found: %d", chainID)
	}

	return client, nil
}

// GetSolanaClient returns the Solana verifier for the given chain ID.
func (mcm *MultiChainManager) GetSolanaClient(chainID int64) (*SolanaVerifier, error) {
	verifier, exists := mcm.solanaClients[chainID]
	if !exists {
		// Check if it's an EVM chain
		if _, evmExists := mcm.clients[chainID]; evmExists {
			return nil, fmt.Errorf("Solana chain client not found: %d is an EVM chain", chainID)
		}
		return nil, fmt.Errorf("Solana chain client not found: %d", chainID)
	}
	return verifier, nil
}

// SetRateLimiter sets an RPC rate limiter applied to all new EVM clients.
func (mcm *MultiChainManager) SetRateLimiter(rl *RPCRateLimiter) {
	mcm.rateLimiter = rl
}

// GetChainConfig gets the configuration for a chain
func (mcm *MultiChainManager) GetChainConfig(chainID int64) (*ChainConfig, error) {
	config, exists := SupportedChains[chainID]
	if !exists {
		mcm.logger.Error("Chain not supported",
			zap.Int64("chain_id", chainID))
		return nil, fmt.Errorf("chain not supported: %d", chainID)
	}

	return config, nil
}

// GetSupportedChains gets all supported chains
func (mcm *MultiChainManager) GetSupportedChains() []*ChainConfig {
	chains := make([]*ChainConfig, 0, len(SupportedChains))
	for _, config := range SupportedChains {
		chains = append(chains, config)
	}
	return chains
}

// GetRPCStatuses returns the runtime RPC status for each configured chain.
func (mcm *MultiChainManager) GetRPCStatuses() map[int64][]RPCStatus {
	statuses := make(map[int64][]RPCStatus, len(mcm.clients))
	for chainID, client := range mcm.clients {
		statuses[chainID] = client.GetRPCStatuses()
	}
	return statuses
}

// GetTestnetChains gets all testnet chains
func (mcm *MultiChainManager) GetTestnetChains() []*ChainConfig {
	chains := make([]*ChainConfig, 0)
	for _, config := range SupportedChains {
		if config.IsTestnet {
			chains = append(chains, config)
		}
	}
	sort.Slice(chains, func(i, j int) bool { return chains[i].ID < chains[j].ID })
	return chains
}

// GetMainnetChains gets all mainnet chains
func (mcm *MultiChainManager) GetMainnetChains() []*ChainConfig {
	chains := make([]*ChainConfig, 0)
	for _, config := range SupportedChains {
		if !config.IsTestnet {
			chains = append(chains, config)
		}
	}
	sort.Slice(chains, func(i, j int) bool { return chains[i].ID < chains[j].ID })
	return chains
}

// Close closes all chain connections
func (mcm *MultiChainManager) Close() {
	mcm.logger.Info("Closing all chain connections")

	for chainID, client := range mcm.clients {
		client.Close()
		mcm.logger.Info("Chain connection closed",
			zap.Int64("chain_id", chainID))
	}

	mcm.clients = make(map[int64]*ChainClient)
}

// CrossChainBridge represents a cross-chain bridge (placeholder for future)
type CrossChainBridge struct {
	logger *zap.Logger
}

// NewCrossChainBridge creates a new cross-chain bridge
func NewCrossChainBridge(logger *zap.Logger) *CrossChainBridge {
	return &CrossChainBridge{
		logger: logger,
	}
}

// BridgeAsset bridges an asset between chains (placeholder)
func (ccb *CrossChainBridge) BridgeAsset(fromChain, toChain int64, asset, amount string) error {
	ccb.logger.Info("Bridging asset",
		zap.Int64("from_chain", fromChain),
		zap.Int64("to_chain", toChain),
		zap.String("asset", asset),
		zap.String("amount", amount))

	// TODO: Implement cross-chain bridge
	return fmt.Errorf("cross-chain bridge not yet implemented")
}
