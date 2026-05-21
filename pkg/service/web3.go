package service

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/web3"

	"go.uber.org/zap"
)

// ErrNotSupported is defined in errors.go

// Web3Deps holds the injectable dependencies for Web3Service.
type Web3Deps struct {
	ChainManager web3.ChainManagerInterface
	SigVerifier  web3.SignatureVerifierInterface
	SolanaVerif  web3.SolanaVerifierInterface
	EIP712Verif  web3.EIP712VerifierInterface
}

// DefaultWeb3Deps creates default real dependencies for production use.
func DefaultWeb3Deps(cfg *config.Config, logger *zap.Logger) Web3Deps {
	mcm := web3.NewMultiChainManager(logger)
	if len(cfg.Web3.Chains) > 0 {
		web3.ApplyChainConfigs(cfg.Web3.Chains)
	}
	return Web3Deps{
		ChainManager: mcm,
		SigVerifier:  web3.NewSignatureVerifier(logger),
		SolanaVerif:  web3.NewSolanaVerifier(logger, cfg.Web3.SolanaRPC),
		EIP712Verif:  web3.NewEIP712Verifier(logger.Named("eip712")),
	}
}

// Web3Service provides Web3 functionality
type Web3Service struct {
	config             *config.Config
	logger             *zap.Logger
	multiChainManager  web3.ChainManagerInterface
	signatureVerifier  web3.SignatureVerifierInterface
	walletManager      *web3.WalletManager
	solanaVerifier     web3.SolanaVerifierInterface
	eip712Verifier     web3.EIP712VerifierInterface
	gasMonitor         *web3.GasMonitor
	cancelGasMonitor   context.CancelFunc
	ipfsClient         *web3.IPFSClient
	transactionQueue   *web3.TransactionQueue
	nonceManagers      map[int64]web3.NonceProvider
	nonceMu            sync.Mutex
	secureKey          *web3.SecurePrivateKey // XOR-encrypted signing key
	txLifecycleManager *web3.TxLifecycleManager
	cancelTxLifecycle  context.CancelFunc
	nftService         *NFTService
	nftAccessCache     middleware.NFTAccessCache
	eventIndexer       *web3.EventIndexer
	cancelEventIndexer context.CancelFunc
	eventListener      *web3.EventListener
	wg                 sync.WaitGroup // tracks background goroutines
}

// NewWeb3Service creates a new Web3 service
func NewWeb3Service(deps Web3Deps, cfg *config.Config, logger *zap.Logger) (*Web3Service, error) {
	logger.Info("Initializing Web3 service")

	service := &Web3Service{
		config:            cfg,
		logger:            logger,
		multiChainManager: deps.ChainManager,
		signatureVerifier: deps.SigVerifier,
		walletManager:     web3.NewWalletManager(logger),
		solanaVerifier:    deps.SolanaVerif,
		eip712Verifier:    deps.EIP712Verif,
		transactionQueue:  web3.NewTransactionQueue(1000),
		nonceManagers:     make(map[int64]web3.NonceProvider),
	}

	// Initialize primary chain (Ethereum)
	if err := service.multiChainManager.AddChain(11155111); err != nil {
		logger.Warn("Failed to add Ethereum Sepolia", zap.Error(err))
	}

	// Initialize Polygon testnet
	if err := service.multiChainManager.AddChain(80002); err != nil {
		logger.Warn("Failed to add Polygon Amoy", zap.Error(err))
	}

	// Initialize Solana chains
	if err := service.multiChainManager.AddChain(-1); err != nil {
		logger.Warn("Failed to add Solana Mainnet", zap.Error(err))
	}
	if err := service.multiChainManager.AddChain(-2); err != nil {
		logger.Warn("Failed to add Solana Devnet", zap.Error(err))
	}

	// Apply RPC rate limiter if configured
	if rl := web3.NewRateLimiterFromConfig(web3.RateLimiterConfig{
		Enabled: cfg.Web3.RateLimit.Enabled,
		Rate:    cfg.Web3.RateLimit.Rate,
		Burst:   cfg.Web3.RateLimit.Burst,
	}, logger); rl != nil {
		service.multiChainManager.SetRateLimiter(rl)
	}

	// Initialize secure private key if configured
	if cfg.Web3.Transaction.PrivateKeyHex != "" {
		sk, err := web3.NewSecurePrivateKeyFromHex(cfg.Web3.Transaction.PrivateKeyHex)
		if err != nil {
			return nil, fmt.Errorf("failed to create secure private key: %w", err)
		}
		service.secureKey = sk
		logger.Info("Secure private key initialized")
	}

	// Initialize GasMonitor with FeeHistory support (EIP-1559) on the first
	// available EVM chain client. If the RPC is unreachable, we log a warning
	// and leave gasMonitor nil — GetGasPriceLevels will return an error.
	// The GasMonitor goroutine runs on a long-lived context; only the initial
	// price fetch uses a short timeout so a hung RPC doesn't block startup.
	if client, err := service.multiChainManager.GetClient(11155111); err == nil {
		gm := web3.NewGasMonitorWithFeeHistory(client, logger, 10, []float64{25, 50, 75})
		gmCtx, gmCancel := context.WithCancel(context.Background())
		// Use a short timeout only for the initial gas price fetch.
		initCtx, initCancel := context.WithTimeout(gmCtx, 15*time.Second)
		if err := gm.Start(initCtx); err != nil {
			logger.Warn("GasMonitor failed to start (RPC may be unavailable)", zap.Error(err))
			gmCancel()
		} else {
			service.gasMonitor = gm
			service.cancelGasMonitor = gmCancel
			logger.Info("GasMonitor started with EIP-1559 FeeHistory support")
		}
		initCancel()
	}

	// Initialize TxLifecycleManager for automated tx monitoring (bump/cancel).
	// Requires both a ChainClient and a private key for signing replacement txs.
	if service.secureKey != nil {
		if client, err := service.multiChainManager.GetClient(11155111); err == nil {
			lifecycleConfig := web3.DefaultTxLifecycleConfig()
			service.txLifecycleManager = web3.NewTxLifecycleManager(
				client, service.secureKey, lifecycleConfig, logger,
			)
			lcCtx, lcCancel := context.WithCancel(context.Background())
			service.cancelTxLifecycle = lcCancel
			service.wg.Add(1)
			go func() {
				defer service.wg.Done()
				service.txLifecycleManager.Start(lcCtx)
			}()
			logger.Info("TxLifecycleManager started")
		}
	}

	// Initialize ReorgDetector + EventIndexer + EventListener for real-time
	// Transfer event monitoring and NFT cache invalidation.
	if client, err := service.multiChainManager.GetClient(11155111); err == nil {
		ethClient := client.GetEthClient()

		// ReorgDetector tracks block headers for reorg detection
		reorgDetector := web3.NewReorgDetector(ethClient, logger)

		// EventIndexer polls for ERC-721/ERC-1155 Transfer events
		transferSig := "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
		transferSingleSig := "0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62"
		indexer, err := web3.NewEventIndexerWithConfig(
			ethClient,
			web3.EventIndexerConfig{
				EventSignatures:    []string{transferSig, transferSingleSig},
				ConfirmationBlocks: 12,
				UpdateInterval:     15 * time.Second,
			},
			logger,
		)
		if err != nil {
			logger.Warn("Failed to create EventIndexer", zap.Error(err))
		} else {
			if cfg.Web3.EthereumWSURL != "" {
				wsSub := web3.NewLogSubscriber(cfg.Web3.EthereumWSURL, logger)
				indexer.SetSubscriber(wsSub)
			}
			indexer.SetReorgDetector(reorgDetector)
			indexerCtx, indexerCancel := context.WithCancel(context.Background())
			if err := indexer.Start(indexerCtx); err != nil {
				indexerCancel()
				logger.Warn("EventIndexer failed to start", zap.Error(err))
			} else {
				service.eventIndexer = indexer
				service.cancelEventIndexer = indexerCancel

				nftSvc, nftErr := NewNFTServiceWithCaller(ethClient, "", nil)
				if nftErr != nil {
					logger.Warn("Failed to create NFTService for event handling", zap.Error(nftErr))
				} else {
					nftSvc.SetLogger(logger)
					service.nftService = nftSvc

					listener := web3.NewEventListener(indexer, logger)
					nftSvc.RegisterEventHandlerWithCache(listener, service.nftAccessCache, 11155111)
					service.eventListener = listener

					indexer.SetOnEvent(func(ctx context.Context, event *web3.IndexedEvent) error {
						return listener.Emit(ctx, event)
					})

					logger.Info("EventIndexer + NFTEventHandler started")
				}
			}
		}
	}

	logger.Info("Web3 service initialized")
	return service, nil
}

// GetMultiChainManager returns the multi-chain manager
func (ws *Web3Service) GetMultiChainManager() web3.ChainManagerInterface {
	return ws.multiChainManager
}

// SetNFTAccessCache sets the NFT access cache
func (ws *Web3Service) SetNFTAccessCache(cache middleware.NFTAccessCache) {
	ws.nftAccessCache = cache
}

// GetEventIndexer returns the event indexer
func (ws *Web3Service) GetEventIndexer() *web3.EventIndexer {
	return ws.eventIndexer
}

// GetSignatureVerifier returns the signature verifier
func (ws *Web3Service) GetSignatureVerifier() web3.SignatureVerifierInterface {
	return ws.signatureVerifier
}

// GetWalletManager returns the wallet manager
func (ws *Web3Service) GetWalletManager() *web3.WalletManager {
	return ws.walletManager
}

// GetGasMonitor returns the gas monitor
func (ws *Web3Service) GetGasMonitor() *web3.GasMonitor {
	return ws.gasMonitor
}

// GetIPFSClient returns the IPFS client
func (ws *Web3Service) GetIPFSClient() *web3.IPFSClient {
	return ws.ipfsClient
}

// GetTransactionQueue returns the transaction queue
func (ws *Web3Service) GetTransactionQueue() *web3.TransactionQueue {
	return ws.transactionQueue
}

// GetEIP712Verifier returns the EIP-712 typed data verifier
func (ws *Web3Service) GetEIP712Verifier() web3.EIP712VerifierInterface {
	return ws.eip712Verifier
}

// getNonceManager returns (or lazily creates) the NonceProvider for the given chain.
func (ws *Web3Service) getNonceManager(chainID int64, client *web3.ChainClient) web3.NonceProvider {
	ws.nonceMu.Lock()
	defer ws.nonceMu.Unlock()
	if nm, ok := ws.nonceManagers[chainID]; ok {
		return nm
	}
	nm := web3.NewNonceManager(client, ws.logger)
	ws.nonceManagers[chainID] = nm
	return nm
}

// GetSupportedChains gets all supported chains
func (ws *Web3Service) GetSupportedChains() []*web3.ChainConfig {
	return ws.multiChainManager.GetSupportedChains()
}

// GetRPCStatuses returns the runtime RPC status for configured chains.
func (ws *Web3Service) GetRPCStatuses() map[int64][]web3.RPCStatus {
	return ws.multiChainManager.GetRPCStatuses()
}

// GetTestnetChains gets all testnet chains
func (ws *Web3Service) GetTestnetChains() []*web3.ChainConfig {
	return ws.multiChainManager.GetTestnetChains()
}

// GetMainnetChains gets all mainnet chains
func (ws *Web3Service) GetMainnetChains() []*web3.ChainConfig {
	return ws.multiChainManager.GetMainnetChains()
}

// GetBalance gets the native token balance for an address on the given chain.
func (ws *Web3Service) GetBalance(ctx context.Context, chainID int64, address string) (*big.Int, error) {
	ws.logger.Debug("Getting balance", zap.Int64("chain_id", chainID), zap.String("address", address))

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	balance, err := client.GetBalance(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	return balance, nil
}

// CreateNFT is not supported — NFT minting is an on-chain write operation
// that requires a private key and gas, which this service does not manage.
func (ws *Web3Service) CreateNFT(ctx context.Context, nft interface{}) error {
	return ErrNotSupported
}

// IsChainSupported checks if a chain is supported
func (ws *Web3Service) IsChainSupported(ctx context.Context, chainID int64) (bool, error) {
	ws.logger.Debug("Checking chain support", zap.Int64("chain_id", chainID))

	chains := ws.multiChainManager.GetSupportedChains()
	for _, chain := range chains {
		if chain.ID == chainID {
			return true, nil
		}
	}

	return false, nil
}

// GetSolanaVerifier returns the Solana verifier for NFT queries.
func (ws *Web3Service) GetSolanaVerifier() web3.SolanaVerifierInterface {
	return ws.solanaVerifier
}

// GetSolanaSigner returns the Solana verifier as a signature signer.
// Returns nil if the underlying verifier does not support signing.
func (ws *Web3Service) GetSolanaSigner() web3.SolanaSigner {
	signer, _ := ws.solanaVerifier.(web3.SolanaSigner)
	return signer
}

// Close closes the Web3 service
func (ws *Web3Service) Close() {
	ws.logger.Info("Closing Web3 service")

	// Cancel all long-lived contexts first so goroutines can unblock
	// from any in-flight RPC calls before we signal them to stop.
	if ws.cancelGasMonitor != nil {
		ws.cancelGasMonitor()
	}
	if ws.cancelTxLifecycle != nil {
		ws.cancelTxLifecycle()
	}
	if ws.cancelEventIndexer != nil {
		ws.cancelEventIndexer()
	}

	// Now signal the stop channels and wait for goroutine completion.
	if ws.gasMonitor != nil {
		ws.gasMonitor.Stop()
	}
	if ws.txLifecycleManager != nil {
		ws.txLifecycleManager.Stop()
	}
	if ws.eventIndexer != nil {
		ws.eventIndexer.Stop()
	}
	if ws.nftService != nil {
		ws.nftService.Close()
	}
	if ws.secureKey != nil {
		ws.secureKey = nil
	}
	if ws.multiChainManager != nil {
		ws.multiChainManager.Close()
	}
	if ws.solanaVerifier != nil {
		ws.solanaVerifier.Close()
	}

	ws.wg.Wait()

	ws.logger.Info("Web3 service closed")
}

// isNonceError classifies nonce-related send failures so the NonceManager
// can self-heal instead of blindly rolling back a stale value.
func isNonceError(err error) (nonceTooLow, replacementFeeTooLow bool) {
	if err == nil {
		return false, false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "nonce too low") {
		return true, false
	}
	if strings.Contains(msg, "replacement fee too low") || strings.Contains(msg, "already known") {
		return false, true
	}
	return false, false
}
