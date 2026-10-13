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
	txtx "github.com/rtcdance/streamgate/pkg/web3/tx"

	"go.uber.org/zap"
)

// Event signature topics (keccak256 of the event signature) the indexer
// subscribes to.
const (
	transferTopic       = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef" // ERC-20/721 Transfer(address,address,uint256)
	transferSingleTopic = "0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62" // ERC-1155 TransferSingle
	transferBatchTopic  = "0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb" // ERC-1155 TransferBatch
)

// eventFilterVersion labels the indexer's durable checkpoint key so a
// checkpoint written under one filter generation is never reused by another.
const eventFilterVersion = "v1"

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

// Web3Option customises Web3Service construction.
type Web3Option func(*Web3Service)

// WithNonceLedger installs the DB-backed shared nonce ledger. When one is
// provided (database available), the SendTransaction nonce path allocates
// through it for its chain instead of the in-memory NonceManager, so the
// deployer account has a SINGLE nonce authority across processes (SG-13 A.3).
// Without a ledger — demo/unit runs with no database — the in-memory manager
// keeps its previous behaviour.
func WithNonceLedger(ledger *txtx.NonceLedger) Web3Option {
	return func(ws *Web3Service) {
		ws.nonceLedger = ledger
	}
}

// GatingRuleFilterRefresher lists the contract addresses of every active
// gating rule on a chain. It is the authority for the event indexer's contract
// whitelist (SG-13 B.1): only contracts some rule actually gates may be
// scanned. *GatingRuleService satisfies it.
type GatingRuleFilterRefresher interface {
	ListActiveContractAddresses(ctx context.Context, chainID int64) ([]string, error)
}

// WithGatingRules installs the gating-rule whitelist authority. Without it the
// indexer cannot build a whitelist and is not started (full-chain scanning is
// forbidden).
func WithGatingRules(rules GatingRuleFilterRefresher) Web3Option {
	return func(ws *Web3Service) {
		ws.gatingRules = rules
	}
}

// WithEventStore installs the event/checkpoint store. A database-backed store
// makes events and checkpoints durable (SG-13 B); a nil store degrades to the
// in-memory one, whose checkpoints do not survive a restart.
func WithEventStore(store web3.EventStore) Web3Option {
	return func(ws *Web3Service) {
		ws.eventStore = store
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
	transactionQueue   *web3.TransactionQueue
	nonceManagers      map[int64]web3.NonceProvider
	nonceMu            sync.Mutex
	nonceLedger        *txtx.NonceLedger
	secureKey          *web3.SecurePrivateKey // XOR-encrypted signing key
	txLifecycleManager *web3.TxLifecycleManager
	cancelTxLifecycle  context.CancelFunc
	nftService         *NFTService
	nftAccessCache     middleware.NFTAccessCache
	// gatingRules is the whitelist authority for the event indexer; nil means no
	// whitelist source exists, so the indexer is not started (SG-13 B.1).
	gatingRules        GatingRuleFilterRefresher
	eventStore         web3.EventStore
	eventIndexer       *web3.EventIndexer
	eventChainID       int64
	eventListener      *web3.EventListener
	cancelEventIndexer context.CancelFunc
	reorgDetector      *web3.ReorgDetector
	reorgChainID       int64
	reorgHandlerMu     sync.RWMutex
	// reorgHandler is notified when a reorg is detected on the indexed chain, so
	// cached authorization evidence for that chain can be dropped. Registering
	// it is optional; the gate also invalidates on a block-hash mismatch.
	reorgHandler func(chainID int64, contracts []string)
	wg           sync.WaitGroup // tracks background goroutines
}

// SetReorgHandler registers a callback invoked when a reorg is detected on the
// indexed chain. It must be called before traffic starts; a handler registered
// later simply misses earlier events.
func (ws *Web3Service) SetReorgHandler(fn func(chainID int64, contracts []string)) {
	ws.reorgHandlerMu.Lock()
	defer ws.reorgHandlerMu.Unlock()
	ws.reorgHandler = fn
}

func (ws *Web3Service) notifyReorg(contracts []string) {
	ws.reorgHandlerMu.RLock()
	fn := ws.reorgHandler
	chainID := ws.reorgChainID
	ws.reorgHandlerMu.RUnlock()
	if fn != nil {
		fn(chainID, contracts)
	}
}

// NewWeb3Service creates a new Web3 service
func NewWeb3Service(deps Web3Deps, cfg *config.Config, logger *zap.Logger, opts ...Web3Option) (*Web3Service, error) {
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
	for _, opt := range opts {
		opt(service)
	}

	// Initialize primary chain (Ethereum)
	if err := service.multiChainManager.AddChain(11155111); err != nil {
		logger.Warn("Failed to add Ethereum Sepolia", zap.Error(err))
	}

	// Initialize Polygon testnet
	if err := service.multiChainManager.AddChain(80002); err != nil {
		logger.Warn("Failed to add Polygon Amoy", zap.Error(err))
	}

	// Initialize Anvil local (dev only)
	if err := service.multiChainManager.AddChain(31337); err != nil {
		logger.Warn("Failed to add Anvil local chain", zap.Error(err))
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
	indexerChainID := cfg.Web3.ChainID
	if indexerChainID == 0 {
		indexerChainID = 11155111
	}
	if client, err := service.multiChainManager.GetClient(indexerChainID); err == nil {
		ethClient := client.GetEthClient()

		// ReorgDetector tracks block headers for reorg detection
		reorgDetector := web3.NewReorgDetector(ethClient, logger)
		// Without a callback the detector would detect a reorg and discard it.
		// The handler drops cached authorization evidence for this chain.
		reorgDetector.SetReorgCallback(service.notifyReorg)
		service.reorgDetector = reorgDetector
		service.reorgChainID = indexerChainID
		service.eventChainID = indexerChainID

		// The whitelist is the set of contracts some active gating rule actually
		// gates. An empty set must not become a full-chain scan: the indexer is
		// simply not started (SG-13 B.1 — log and skip, not process exit).
		whitelist, wlErr := service.contractAddressesFromRules(indexerChainID)
		if wlErr != nil {
			logger.Warn("Event indexer not started: could not read the gating-rule contract whitelist",
				zap.Error(wlErr))
		} else if len(whitelist) == 0 {
			logger.Info("Event indexer not started: no active gating-rule contracts to scan")
		} else {
			// ERC-20/721 Transfer, ERC-1155 TransferSingle, ERC-1155 TransferBatch.
			indexer, err := web3.NewEventIndexerWithConfig(
				ethClient,
				web3.EventIndexerConfig{
					ContractAddresses:  whitelist,
					EventSignatures:    []string{transferTopic, transferSingleTopic, transferBatchTopic},
					ChainID:            indexerChainID,
					FilterVersion:      eventFilterVersion,
					ConfirmationBlocks: 12,
					UpdateInterval:     4 * time.Second,
				},
				logger,
			)
			if err != nil {
				logger.Warn("Failed to create EventIndexer", zap.Error(err))
			} else {
				var wsSub *web3.LogSubscriber
				if cfg.Web3.EthereumWSURL != "" {
					wsSub = web3.NewLogSubscriber(cfg.Web3.EthereumWSURL, logger)
					indexer.SetSubscriber(wsSub)
				}
				indexer.SetReorgDetector(reorgDetector)
				// A database-backed store keeps events, checkpoints and the block
				// journal durable across restarts (SG-13 B); without one the
				// in-memory store is a degraded mode whose checkpoints die with
				// the process.
				store := service.eventStore
				if store == nil {
					logger.Warn("Event indexer uses an in-memory event store: checkpoints do not survive a restart")
					store = web3.NewMemoryEventStore()
				}
				indexer.SetEventStore(store)
				indexerCtx, indexerCancel := context.WithCancel(context.Background())
				if err := indexer.Start(indexerCtx); err != nil {
					indexerCancel()
					if wsSub != nil {
						wsSub.Unsubscribe()
					}
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
						nftSvc.RegisterEventHandlerWithCache(listener, service.nftAccessCache, indexerChainID)
						service.eventListener = listener

						indexer.SetOnEvent(func(ctx context.Context, event *web3.IndexedEvent) error {
							return listener.Emit(ctx, classifyTransferEvent(event))
						})

						logger.Info("EventIndexer + NFTEventHandler started")
					}
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

// SetNFTAccessCache sets the NFT access cache. When the event handler is
// already running it is re-registered against the new cache so the handler
// does not keep invalidating a cache nothing reads.
func (ws *Web3Service) SetNFTAccessCache(cache middleware.NFTAccessCache) {
	ws.nftAccessCache = cache
	if ws.eventListener != nil && ws.nftService != nil {
		ws.nftService.RegisterEventHandlerWithCache(ws.eventListener, cache, ws.eventChainID)
	}
}

// contractAddressesFromRules reads the indexer's contract whitelist from the
// active gating rules. Without a configured source (no database) there is no
// whitelist, and no whitelist means the indexer must not scan anything.
func (ws *Web3Service) contractAddressesFromRules(chainID int64) ([]string, error) {
	if ws.gatingRules == nil {
		return nil, fmt.Errorf("no gating-rule whitelist source configured")
	}
	return ws.gatingRules.ListActiveContractAddresses(context.Background(), chainID)
}

// RefreshEventFilter re-reads the active gating-rule contract whitelist and
// re-subscribes the indexer to it (bounded catch-up from the old checkpoint).
// It is the SG-13 B hook for rule create/update/delete: a rule that starts
// gating a contract must make that contract's Transfer events visible to the
// invalidation pipeline. It returns an error when no whitelist source or no
// running indexer exists, or when the new whitelist is empty (which would be a
// full-chain scan).
func (ws *Web3Service) RefreshEventFilter(ctx context.Context) error {
	if ws.eventIndexer == nil {
		return fmt.Errorf("event indexer is not running")
	}
	addresses, err := ws.contractAddressesFromRules(ws.eventChainID)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		return fmt.Errorf("gating-rule contract whitelist is empty")
	}
	return ws.eventIndexer.UpdateFilter(ctx, addresses)
}

// classifyTransferEvent gives a raw indexed event its canonical Transfer
// event-type name when its topic matches a known ERC-20/721/1155 transfer
// signature, so listener.Emit routes it to the registered handlers. Indexed
// events are otherwise typed "ContractEvent" regardless of topic.
func classifyTransferEvent(event *web3.IndexedEvent) *web3.IndexedEvent {
	if event == nil || len(event.Topics) == 0 {
		return event
	}
	topic := strings.ToLower(event.Topics[0])
	switch topic {
	case transferTopic:
		event.EventType = "Transfer"
	case transferSingleTopic:
		event.EventType = "TransferSingle"
	}
	return event
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
	// The DB ledger is the single nonce authority for its chain (SG-13 A.3):
	// every allocator in this process and in other instances draws from the
	// same Postgres reservations table, so the deployer account can never be
	// handed the same nonce twice. Chains the ledger does not cover keep the
	// in-memory manager.
	if ws.nonceLedger != nil && ws.nonceLedger.ChainID() == chainID {
		ws.nonceManagers[chainID] = ws.nonceLedger
		return ws.nonceLedger
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
