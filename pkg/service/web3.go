package service

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"streamgate/pkg/core/config"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// ErrNotSupported is defined in errors.go

// Web3Deps holds the injectable dependencies for Web3Service.
type Web3Deps struct {
	ChainManager web3.ChainManagerInterface
	SigVerifier  web3.SignatureVerifierInterface
	SolanaVerif  web3.SolanaVerifierInterface
}

// DefaultWeb3Deps creates default real dependencies for production use.
func DefaultWeb3Deps(cfg *config.Config, logger *zap.Logger) Web3Deps {
	return Web3Deps{
		ChainManager: web3.NewMultiChainManager(logger),
		SigVerifier:  web3.NewSignatureVerifier(logger),
		SolanaVerif:  web3.NewSolanaVerifier(logger, cfg.Web3.SolanaRPC),
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
	gasMonitor         *web3.GasMonitor
	cancelGasMonitor   context.CancelFunc
	ipfsClient         *web3.IPFSClient
	contractInteractor *web3.ContractInteractor //nolint:unused
	transactionQueue   *web3.TransactionQueue
	nonceManagers      map[int64]web3.NonceProvider // chainID → nonce manager
	nonceMu            sync.Mutex
	secureKey          *web3.SecurePrivateKey // XOR-encrypted signing key
	txLifecycleManager *web3.TxLifecycleManager
	cancelTxLifecycle  context.CancelFunc
	nftService         *NFTService
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
		indexer, err := web3.NewEventIndexerWithConfig(
			ethClient,
			web3.EventIndexerConfig{
				EventSignatures:    []string{transferSig},
				ConfirmationBlocks: 12,
				UpdateInterval:     15 * time.Second,
			},
			logger,
			cfg.Web3.EthereumWSURL, // pass WS URL for real-time event subscriptions
		)
		if err != nil {
			logger.Warn("Failed to create EventIndexer", zap.Error(err))
		} else {
			indexer.SetReorgDetector(reorgDetector)
			indexerCtx, indexerCancel := context.WithCancel(context.Background())
			if err := indexer.Start(indexerCtx); err != nil {
				indexerCancel()
				logger.Warn("EventIndexer failed to start", zap.Error(err))
			} else {
				service.eventIndexer = indexer
				service.cancelEventIndexer = indexerCancel

				// Create NFTService and wire up event-driven cache invalidation
				nftSvc, nftErr := NewNFTServiceWithCaller(ethClient, "", nil)
				if nftErr != nil {
					logger.Warn("Failed to create NFTService for event handling", zap.Error(nftErr))
				} else {
					nftSvc.SetLogger(logger)
					service.nftService = nftSvc

					listener := web3.NewEventListener(indexer, logger)
					nftSvc.RegisterEventHandler(listener)
					service.eventListener = listener

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

// VerifySignature verifies a message signature
func (ws *Web3Service) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	ws.logger.Debug("Verifying signature", zap.String("address", address))
	return ws.signatureVerifier.VerifySignature(ctx, address, message, signature)
}

// VerifyNFTOwnership verifies NFT ownership on the given chain.
// For EVM chains (positive chainID) it uses ERC-721/1155 ownership checks.
// For Solana chains (negative chainID) it derives the Associated Token Account
// from (owner, mint) and verifies on-chain ownership via RPC.
func (ws *Web3Service) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	ctx, span := monitoring.StartOTelSpan(ctx, "web3.verify_nft_ownership",
		attribute.Int64("chain_id", chainID),
		attribute.String("contract", contractAddress),
		attribute.String("token_id", tokenID),
	)
	defer span.End()

	ws.logger.Debug("Verifying NFT ownership",
		zap.Int64("chain_id", chainID),
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	// Solana path: negative chain IDs
	if chainID < 0 {
		solanaClient, err := ws.multiChainManager.GetSolanaClient(chainID)
		if err != nil {
			return false, fmt.Errorf("solana chain client not found for chain %d: %w", chainID, err)
		}

		// Derive the Associated Token Account from (owner, mint)
		tokenAccount, err := solanaClient.DeriveTokenAccountAddress(ownerAddress, contractAddress)
		if err != nil {
			return false, fmt.Errorf("failed to derive token account: %w", err)
		}

		// Verify on-chain token account ownership
		return solanaClient.VerifyTokenAccount(ctx, tokenAccount, ownerAddress)
	}

	// EVM path
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return false, err
	}

	ethCaller := client.GetEthClient()

	// Detect token standard and route to the appropriate verifier
	standard := web3.DetectTokenStandard(ctx, ethCaller, contractAddress, ws.logger)
	switch standard {
	case web3.TokenStandardERC1155:
		verifier := web3.NewERC1155Verifier(ethCaller, ws.logger, nil)
		return verifier.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	default:
		// ERC-721 or unknown — use the standard NFTVerifier
		nftVerifier := web3.NewNFTVerifier(ethCaller, ws.logger)
		return nftVerifier.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
	}
}

// GetNFTBalance gets collection balance for an owner.
// For EVM chains (positive chainID) it uses ERC-721/1155 balanceOf.
// For Solana chains (negative chainID) it verifies the Associated Token Account
// and returns 1 if held, 0 otherwise (Solana NFTs are 1:1 mint→token-account).
func (ws *Web3Service) GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error) {
	ws.logger.Debug("Getting NFT balance",
		zap.Int64("chain_id", chainID),
		zap.String("contract", contractAddress),
		zap.String("owner", ownerAddress))

	// Solana path: negative chain IDs
	if chainID < 0 {
		solanaClient, err := ws.multiChainManager.GetSolanaClient(chainID)
		if err != nil {
			return nil, fmt.Errorf("solana chain client not found for chain %d: %w", chainID, err)
		}

		// Derive the Associated Token Account from (owner, mint)
		tokenAccount, err := solanaClient.DeriveTokenAccountAddress(ownerAddress, contractAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to derive token account: %w", err)
		}

		// Verify on-chain token account ownership
		owned, err := solanaClient.VerifyTokenAccount(ctx, tokenAccount, ownerAddress)
		if err != nil {
			return nil, err
		}
		if owned {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	}

	// EVM path
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, err
	}

	ethCaller := client.GetEthClient()

	// Detect token standard and route to the appropriate verifier
	standard := web3.DetectTokenStandard(ctx, ethCaller, contractAddress, ws.logger)
	switch standard {
	case web3.TokenStandardERC1155:
		verifier := web3.NewERC1155Verifier(ethCaller, ws.logger, nil)
		// For balance checks, use tokenID "0" as default — the caller can
		// specify a specific token via VerifyNFTOwnership if needed.
		owned, err := verifier.VerifyNFTOwnership(ctx, contractAddress, "0", ownerAddress)
		if err != nil {
			return nil, err
		}
		if owned {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	default:
		nftVerifier := web3.NewNFTVerifier(ethCaller, ws.logger)
		balance, err := nftVerifier.GetNFTBalance(ctx, contractAddress, ownerAddress)
		if err != nil {
			return nil, err
		}
		return balance, nil
	}
}

// VerifyNFTOwnershipAutoDetect detects the token standard and routes to the
// correct verification method. For EVM chains it uses NFTVerifier.AutoDetect;
// for Solana chains it falls back to VerifyNFTOwnership.
func (ws *Web3Service) VerifyNFTOwnershipAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	client, err := ws.multiChainManager.GetClient(1)
	if err != nil {
		return false, err
	}
	ethCaller := client.GetEthClient()
	verifier := web3.NewNFTVerifier(ethCaller, ws.logger)
	return verifier.VerifyNFTOwnershipAutoDetect(ctx, contractAddress, tokenID, ownerAddress)
}

// VerifyNFTCollectionAutoDetect detects the token standard and routes to the
// correct collection-level verification.
func (ws *Web3Service) VerifyNFTCollectionAutoDetect(ctx context.Context, contractAddress, ownerAddress string) (bool, error) {
	client, err := ws.multiChainManager.GetClient(1)
	if err != nil {
		return false, err
	}
	ethCaller := client.GetEthClient()
	verifier := web3.NewNFTVerifier(ethCaller, ws.logger)
	return verifier.VerifyNFTCollectionAutoDetect(ctx, contractAddress, ownerAddress)
}

// GetGasPrice gets the current gas price
func (ws *Web3Service) GetGasPrice(ctx context.Context, chainID int64) (string, error) {
	ws.logger.Debug("Getting gas price", zap.Int64("chain_id", chainID))

	// Get chain client
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "", err
	}

	// Get gas price
	gasPrice, err := client.GetGasPrice(ctx)
	if err != nil {
		return "", err
	}

	return gasPrice.String(), nil
}

// GetGasPriceLevels gets gas price levels
func (ws *Web3Service) GetGasPriceLevels(ctx context.Context, chainID int64) ([]*web3.GasPrice, error) {
	ws.logger.Debug("Getting gas price levels", zap.Int64("chain_id", chainID))

	if ws.gasMonitor == nil {
		return nil, fmt.Errorf("gas monitor not initialized")
	}

	return ws.gasMonitor.GetGasPriceLevels(ctx)
}

// UploadToIPFS uploads a file to IPFS
func (ws *Web3Service) UploadToIPFS(ctx context.Context, filename string, data []byte) (string, error) {
	ws.logger.Debug("Uploading to IPFS",
		zap.String("filename", filename),
		zap.Int("size", len(data)))

	if ws.ipfsClient == nil {
		return "", fmt.Errorf("IPFS client not initialized")
	}

	return ws.ipfsClient.UploadFile(ctx, filename, data)
}

// DownloadFromIPFS downloads a file from IPFS
func (ws *Web3Service) DownloadFromIPFS(ctx context.Context, cid string) ([]byte, error) {
	ws.logger.Debug("Downloading from IPFS", zap.String("cid", cid))

	if ws.ipfsClient == nil {
		return nil, fmt.Errorf("IPFS client not initialized")
	}

	return ws.ipfsClient.DownloadFile(ctx, cid)
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

// SendTransaction builds, signs, and sends an EVM transaction on the given chain.
// It resolves the nonce, estimates gas (with configurable buffer), signs with the
// configured private key, and dispatches via ChainClient.SendTransaction.
// Returns the transaction hash on success.
func (ws *Web3Service) SendTransaction(ctx context.Context, chainID int64, to string, value *big.Int, data []byte) (string, error) {
	if ws.secureKey == nil {
		return "", fmt.Errorf("transaction private key not configured")
	}

	// Apply a default timeout so a hung RPC doesn't block indefinitely.
	// If the caller already set a shorter deadline, it takes precedence.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "", fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	var txHash string
	signErr := ws.secureKey.UseKey(func(privateKey *ecdsa.PrivateKey) error {
		publicKey := privateKey.Public().(*ecdsa.PublicKey)
		fromAddress := crypto.PubkeyToAddress(*publicKey)

		// Resolve nonce via NonceManager (avoids duplicate nonces under concurrency)
		nm := ws.getNonceManager(chainID, client)
		nonce, err := nm.NextNonce(ctx, fromAddress.Hex())
		if err != nil {
			return fmt.Errorf("failed to get nonce: %w", err)
		}

		// Pre-send simulation: eth_call to detect reverts before signing.
		// This saves gas by not signing & sending a tx that would definitely fail.
		toAddr := common.HexToAddress(to)
		if len(data) > 0 {
			callMsg := ethereum.CallMsg{
				From:  fromAddress,
				To:    &toAddr,
				Value: value,
				Data:  data,
			}
			ethClient := client.GetEthClient()
			if _, err := ethClient.CallContract(ctx, callMsg, nil); err != nil {
				nm.Rollback(fromAddress.Hex(), nonce)
				return fmt.Errorf("transaction simulation failed: %w", err)
			}
		}

		// Estimate gas
		txConfig := ws.config.Web3.Transaction
		gasLimit := txConfig.GasLimit
		if gasLimit == 0 {
			gasLimit = 200000 // safe default for contract writes
		}
		if len(data) > 0 {
			callMsg := ethereum.CallMsg{
				From: fromAddress,
				To:   &toAddr,
				Data: data,
			}
			estimated, err := client.EstimateGas(ctx, callMsg)
			if err == nil && estimated > 0 {
				gasLimit = estimated
			}
		}
		// Apply gas buffer with overflow protection
		if multiplier := txConfig.GasMultiplier; multiplier > 1 {
			scaled := float64(gasLimit) * multiplier
			if scaled >= float64(math.MaxUint64) {
				return fmt.Errorf("gas limit overflow after applying multiplier %.2f", multiplier)
			}
			gasLimit = uint64(math.Ceil(scaled))
		}

		// Get gas price
		gasPrice, err := client.GetGasPrice(ctx)
		if err != nil {
			return fmt.Errorf("failed to get gas price: %w", err)
		}

		// Build and sign transaction
		chainIDBig := big.NewInt(chainID)
		var signedTx *types.Transaction

		if txConfig.EIP1559 {
			// EIP-1559 dynamic fee transaction
			tipCap, err := client.SuggestGasTipCap(ctx)
			if err != nil {
				return fmt.Errorf("failed to get gas tip cap: %w", err)
			}
			// If MaxPriorityFeePerGasGwei is configured, use it as the tip floor
			if txConfig.MaxPriorityFeePerGasGwei > 0 {
				configuredTip := gweiToWei(txConfig.MaxPriorityFeePerGasGwei)
				if configuredTip.Cmp(tipCap) > 0 {
					tipCap = configuredTip
				}
			}

			// Calculate maxFeePerGas = 2 * baseFee + tipCap
			header, err := client.HeaderByNumber(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to get latest header: %w", err)
			}
			baseFee := header.BaseFee
			var maxFeePerGas *big.Int
			if baseFee != nil {
				maxFeePerGas = new(big.Int).Add(
					new(big.Int).Mul(baseFee, big.NewInt(2)),
					tipCap,
				)
			} else {
				// Chain doesn't support EIP-1559 yet, fall back to legacy gas price
				maxFeePerGas = gasPrice
			}
			// If MaxFeePerGasGwei is configured, use it as the fee cap floor
			if txConfig.MaxFeePerGasGwei > 0 {
				configuredMaxFee := gweiToWei(txConfig.MaxFeePerGasGwei)
				if configuredMaxFee.Cmp(maxFeePerGas) > 0 {
					maxFeePerGas = configuredMaxFee
				}
			}

			// Apply hard cap to prevent malicious RPC from suggesting astronomical fees
			capGwei := txConfig.MaxFeePerGasCapGwei
			if capGwei <= 0 {
				capGwei = 500 // sensible default: 500 Gwei
			}
			feeCap := gweiToWei(capGwei)
			if maxFeePerGas.Cmp(feeCap) > 0 {
				ws.logger.Warn("maxFeePerGas exceeds hard cap, clamping",
					zap.String("estimated", maxFeePerGas.String()),
					zap.String("cap", feeCap.String()))
				maxFeePerGas = feeCap
			}

			unsignedTx := types.NewTx(&types.DynamicFeeTx{
				ChainID:   chainIDBig,
				Nonce:     nonce,
				To:        &toAddr,
				Value:     value,
				Gas:       gasLimit,
				GasFeeCap: maxFeePerGas,
				GasTipCap: tipCap,
				Data:      data,
			})
			signedTx, err = types.SignTx(unsignedTx, types.LatestSignerForChainID(chainIDBig), privateKey)
			if err != nil {
				return fmt.Errorf("failed to sign EIP-1559 transaction: %w", err)
			}

			if err := client.SendTransaction(ctx, signedTx); err != nil {
				if nonceTooLow, replacementFeeTooLow := isNonceError(err); nonceTooLow {
					nm.Reset(fromAddress.Hex())
					ws.logger.Warn("nonce too low on send, resetting nonce tracker",
						zap.String("from", fromAddress.Hex()), zap.Error(err))
				} else if replacementFeeTooLow {
					nm.Rollback(fromAddress.Hex(), nonce)
					ws.logger.Warn("replacement fee too low, rolled back nonce",
						zap.String("from", fromAddress.Hex()), zap.Error(err))
				} else {
					nm.Rollback(fromAddress.Hex(), nonce)
				}
				return fmt.Errorf("failed to send EIP-1559 transaction: %w", err)
			}

			ws.logger.Info("EIP-1559 transaction sent",
				zap.String("tx_hash", signedTx.Hash().Hex()),
				zap.Int64("chain_id", chainID),
				zap.String("from", fromAddress.Hex()),
				zap.String("to", to),
				zap.String("max_fee_per_gas", maxFeePerGas.String()),
				zap.String("tip_cap", tipCap.String()))
		} else {
			// Legacy transaction (EIP-155)
			unsignedTx := types.NewTransaction(nonce, toAddr, value, gasLimit, gasPrice, data)
			signedTx, err = types.SignTx(unsignedTx, types.NewEIP155Signer(chainIDBig), privateKey)
			if err != nil {
				return fmt.Errorf("failed to sign transaction: %w", err)
			}

			if err := client.SendTransaction(ctx, signedTx); err != nil {
				if nonceTooLow, replacementFeeTooLow := isNonceError(err); nonceTooLow {
					nm.Reset(fromAddress.Hex())
					ws.logger.Warn("nonce too low on send, resetting nonce tracker",
						zap.String("from", fromAddress.Hex()), zap.Error(err))
				} else if replacementFeeTooLow {
					nm.Rollback(fromAddress.Hex(), nonce)
					ws.logger.Warn("replacement fee too low, rolled back nonce",
						zap.String("from", fromAddress.Hex()), zap.Error(err))
				} else {
					nm.Rollback(fromAddress.Hex(), nonce)
				}
				return fmt.Errorf("failed to send transaction: %w", err)
			}

			ws.logger.Info("Transaction sent",
				zap.String("tx_hash", signedTx.Hash().Hex()),
				zap.Int64("chain_id", chainID),
				zap.String("from", fromAddress.Hex()),
				zap.String("to", to))
		}

		txHash = signedTx.Hash().Hex()
		return nil
	})

	if signErr != nil {
		return "", signErr
	}
	return txHash, nil
}

// WaitForReceipt polls for a transaction receipt until it is mined or the
// context deadline is exceeded. It optionally waits for N block confirmations.
func (ws *Web3Service) WaitForReceipt(ctx context.Context, chainID int64, txHash string, confirmations uint64) (*web3.ReceiptInfo, error) {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, fmt.Errorf("chain client not found: %w", err)
	}

	// Poll for receipt with 2-second interval
	for {
		receipt, err := client.GetTransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled while waiting for receipt: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}

	receipt, err := client.GetTransactionReceipt(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("receipt disappeared after first sighting: %w", err)
	}

	// Wait for confirmations if requested
	if confirmations > 0 && receipt.Status == 1 {
		originalBlockHash := receipt.BlockHash
		targetBlock := receipt.BlockNumber + confirmations
		for {
			blockNum, err := client.GetBlockNumber(ctx)
			if err == nil && blockNum >= targetBlock {
				break
			}
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled while waiting for confirmations: %w", ctx.Err())
			case <-time.After(3 * time.Second):
			}
		}
		// Re-fetch receipt to confirm it wasn't reorg'd
		receipt, err = client.GetTransactionReceipt(ctx, txHash)
		if err != nil {
			return nil, fmt.Errorf("failed to re-fetch receipt after confirmations: %w", err)
		}
		if receipt.BlockHash != originalBlockHash {
			return nil, fmt.Errorf("reorg detected: receipt block hash changed from %s to %s", originalBlockHash, receipt.BlockHash)
		}
	}

	return receipt, nil
}

// RegisterContent registers content on the ContentRegistry contract on the given chain.
// It packs the ABI call data, sends the transaction, and waits for the receipt.
func (ws *Web3Service) RegisterContent(ctx context.Context, chainID int64, contractAddress, contentHash, metadataURI string) (string, error) {
	txConfig := ws.config.Web3.Transaction
	if txConfig.PrivateKeyHex == "" {
		return "", fmt.Errorf("transaction private key not configured")
	}

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "", fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	// Pack the registerContent ABI call
	registry := &web3.ContractContentRegistry{
		Address: contractAddress,
		ABI:     web3.ContentRegistryABI,
	}
	ci := web3.NewContractInteractor(client.GetEthClient(), ws.logger)
	callData, err := registry.RegisterContent(ctx, ci, contentHash, "", metadataURI)
	if err != nil {
		return "", fmt.Errorf("failed to pack registerContent call: %w", err)
	}

	// Send the transaction
	txHash, err := ws.SendTransaction(ctx, chainID, contractAddress, big.NewInt(0), callData)
	if err != nil {
		return "", fmt.Errorf("failed to send registerContent tx: %w", err)
	}

	// Wait for receipt
	confirmations := txConfig.Confirmations
	if confirmations == 0 {
		confirmations = 2
	}
	receipt, err := ws.WaitForReceipt(ctx, chainID, txHash, confirmations)
	if err != nil {
		return txHash, fmt.Errorf("tx sent (%s) but receipt unavailable: %w", txHash, err)
	}
	if receipt.Status != 1 {
		return txHash, fmt.Errorf("registerContent tx reverted (status=%d)", receipt.Status)
	}

	ws.logger.Info("Content registered on-chain",
		zap.String("tx_hash", txHash),
		zap.String("content_hash", contentHash),
		zap.Uint64("block", receipt.BlockNumber))

	return txHash, nil
}

// gweiToWei converts a Gwei value (float64) to wei (*big.Int).
func gweiToWei(gwei float64) *big.Int {
	fWei := new(big.Float).Mul(big.NewFloat(gwei), big.NewFloat(params.GWei))
	wei, _ := fWei.Int(nil)
	return wei
}

// GetNFT gets NFT metadata by contract and token ID on the given chain.
func (ws *Web3Service) GetNFT(ctx context.Context, chainID int64, contractAddress, tokenID string) (*web3.NFTInfo, error) {
	ws.logger.Debug("Getting NFT", zap.Int64("chain_id", chainID), zap.String("contract", contractAddress), zap.String("token_id", tokenID))

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	nftVerifier := web3.NewNFTVerifier(client.GetEthClient(), ws.logger)
	return nftVerifier.GetNFTInfo(ctx, contractAddress, tokenID)
}

// ListNFTs lists NFTs
func (ws *Web3Service) ListNFTs(ctx context.Context, offset, limit int) ([]interface{}, error) {
	ws.logger.Debug("Listing NFTs", zap.Int("offset", offset), zap.Int("limit", limit))

	return []interface{}{}, nil
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

// GetSolanaVerifier returns the Solana verifier
func (ws *Web3Service) GetSolanaVerifier() web3.SolanaVerifierInterface {
	return ws.solanaVerifier
}

// GetTokenBalance returns the ERC-20 token balance for an address on the given chain.
func (ws *Web3Service) GetTokenBalance(ctx context.Context, chainID int64, contractAddress, accountAddress string) (string, error) {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "", fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	reader := web3.NewERC20Reader(client.GetEthClient(), ws.logger)
	balance, err := reader.GetTokenBalance(ctx, contractAddress, accountAddress)
	if err != nil {
		return "", fmt.Errorf("erc20 balanceOf failed: %w", err)
	}
	return balance.String(), nil
}

// GetTokenAllowance returns the ERC-20 allowance from owner to spender.
func (ws *Web3Service) GetTokenAllowance(ctx context.Context, chainID int64, contractAddress, ownerAddress, spenderAddress string) (string, error) {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "", fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	reader := web3.NewERC20Reader(client.GetEthClient(), ws.logger)
	allowance, err := reader.GetTokenAllowance(ctx, contractAddress, ownerAddress, spenderAddress)
	if err != nil {
		return "", fmt.Errorf("erc20 allowance failed: %w", err)
	}
	return allowance.String(), nil
}

// GetTokenInfo returns ERC-20 token metadata (name, symbol, decimals, totalSupply).
func (ws *Web3Service) GetTokenInfo(ctx context.Context, chainID int64, contractAddress string) (*web3.ERC20TokenInfo, error) {
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return nil, fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	reader := web3.NewERC20Reader(client.GetEthClient(), ws.logger)
	return reader.GetTokenInfo(ctx, contractAddress)
}

// VerifySolanaTokenAccount verifies a Solana token account's owner on-chain.
func (ws *Web3Service) VerifySolanaTokenAccount(ctx context.Context, tokenAccount, ownerAddress string) (bool, error) {
	ws.logger.Debug("Verifying Solana token account",
		zap.String("token_account", tokenAccount),
		zap.String("owner", ownerAddress))

	if ws.solanaVerifier == nil {
		return false, fmt.Errorf("solana verifier not initialized")
	}
	return ws.solanaVerifier.VerifyTokenAccount(ctx, tokenAccount, ownerAddress)
}

// VerifySolanaMintAuthority verifies a Solana mint's authority on-chain.
func (ws *Web3Service) VerifySolanaMintAuthority(ctx context.Context, mintAddress, authorityAddress string) (bool, error) {
	ws.logger.Debug("Verifying Solana mint authority",
		zap.String("mint", mintAddress),
		zap.String("authority", authorityAddress))

	if ws.solanaVerifier == nil {
		return false, fmt.Errorf("solana verifier not initialized")
	}
	return ws.solanaVerifier.VerifyMintAuthority(ctx, mintAddress, authorityAddress)
}

// VerifySolanaMetaplexMetadata verifies Metaplex NFT metadata signature.
func (ws *Web3Service) VerifySolanaMetaplexNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error) {
	ws.logger.Debug("Verifying Solana Metaplex NFT ownership",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress))

	if ws.solanaVerifier == nil {
		return false, fmt.Errorf("solana verifier not initialized")
	}
	return ws.solanaVerifier.VerifyMetaplexNFTOwnership(ctx, mintAddress, ownerAddress)
}

// SubmitPermit submits an EIP-2612 permit transaction to an ERC-20 contract.
// The caller provides the signed permit parameters (v, r, s from EIP-712 signing).
// This completes the gasless approval flow: sign off-chain → submit on-chain.
func (ws *Web3Service) SubmitPermit(ctx context.Context, chainID int64, contractAddress, owner, spender string, value, deadline *big.Int, v uint8, r, s [32]byte) (string, error) {
	ownerAddr := common.HexToAddress(owner)
	spenderAddr := common.HexToAddress(spender)

	callData, err := web3.PackPermitCall(ownerAddr, spenderAddr, value, deadline, v, r, s)
	if err != nil {
		return "", fmt.Errorf("failed to pack permit call: %w", err)
	}

	return ws.SendTransaction(ctx, chainID, contractAddress, big.NewInt(0), callData)
}

// ReplaceStuckTransaction replaces a stuck pending transaction by bumping its
// gas price and resending with the same nonce. The bumpPercent controls how
// much to increase the gas price (e.g. 10 = 10% higher).
func (ws *Web3Service) ReplaceStuckTransaction(ctx context.Context, chainID int64, pending *web3.PendingTx, bumpPercent int64) (string, error) {
	if ws.secureKey == nil {
		return "", fmt.Errorf("transaction private key not configured")
	}

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "", fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	var txHash string
	signErr := ws.secureKey.UseKey(func(privateKey *ecdsa.PrivateKey) error {
		tracker := web3.NewTxTracker(client, ws.logger)
		hash, err := tracker.BumpGas(ctx, privateKey, pending, bumpPercent)
		if err != nil {
			return err
		}
		txHash = hash
		return nil
	})

	if signErr != nil {
		return "", signErr
	}
	return txHash, nil
}

// CancelPendingTransaction cancels a pending transaction by sending a zero-value
// self-transfer with the same nonce but higher gas price.
func (ws *Web3Service) CancelPendingTransaction(ctx context.Context, chainID int64, pending *web3.PendingTx, bumpPercent int64) (string, error) {
	if ws.secureKey == nil {
		return "", fmt.Errorf("transaction private key not configured")
	}

	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return "", fmt.Errorf("chain client not found for chain %d: %w", chainID, err)
	}

	var txHash string
	signErr := ws.secureKey.UseKey(func(privateKey *ecdsa.PrivateKey) error {
		tracker := web3.NewTxTracker(client, ws.logger)
		hash, err := tracker.CancelTx(ctx, privateKey, pending, bumpPercent)
		if err != nil {
			return err
		}
		txHash = hash
		return nil
	})

	if signErr != nil {
		return "", signErr
	}
	return txHash, nil
}

// VerifyMerkleWhitelist verifies that an address is included in a Merkle
// whitelist. This is the standard pattern for NFT airdrop/whitelist claims:
// the dApp submits a Merkle proof from the front-end, and the backend
// verifies it against a known root before granting access.
func (ws *Web3Service) VerifyMerkleWhitelist(rootHex, address string, proofHex []string) (bool, error) {
	rootBytes, err := hex.DecodeString(strings.TrimPrefix(rootHex, "0x"))
	if err != nil {
		return false, fmt.Errorf("invalid root hex: %w", err)
	}
	var root [32]byte
	copy(root[:], rootBytes)

	// Hash the address as the leaf (matches on-chain: keccak256(abi.encodePacked(address)))
	// Uses 20-byte binary address encoding to match OpenZeppelin's MerkleProof.verify
	leaf := web3.HashLeaf(common.HexToAddress(address).Bytes())

	// Decode proof elements
	proof := make([][32]byte, len(proofHex))
	for i, p := range proofHex {
		b, err := hex.DecodeString(strings.TrimPrefix(p, "0x"))
		if err != nil {
			return false, fmt.Errorf("invalid proof element %d: %w", i, err)
		}
		copy(proof[i][:], b)
	}

	return web3.VerifyMerkleProof(root, leaf, proof), nil
}

// Close closes the Web3 service
func (ws *Web3Service) Close() {
	ws.logger.Info("Closing Web3 service")

	if ws.gasMonitor != nil {
		ws.gasMonitor.Stop()
	}
	if ws.cancelGasMonitor != nil {
		ws.cancelGasMonitor()
	}
	if ws.txLifecycleManager != nil {
		ws.txLifecycleManager.Stop()
	}
	if ws.cancelTxLifecycle != nil {
		ws.cancelTxLifecycle()
	}
	if ws.eventIndexer != nil {
		ws.eventIndexer.Stop()
	}
	if ws.cancelEventIndexer != nil {
		ws.cancelEventIndexer()
	}
	if ws.nftService != nil {
		ws.nftService.Close()
	}
	if ws.secureKey != nil {
		// Best-effort zeroize; SecurePrivateKey has no explicit Zeroize method,
		// but the encKey/xorPad will be GC'd. UseKey already zeroes after each use.
		ws.secureKey = nil
	}
	if ws.multiChainManager != nil {
		ws.multiChainManager.Close()
	}
	if ws.solanaVerifier != nil {
		ws.solanaVerifier.Close()
	}

	// Wait for all spawned goroutines to finish after their cancel signals.
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
