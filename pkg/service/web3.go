package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core/config"
	"streamgate/pkg/web3"
)

// Web3Service provides Web3 functionality
type Web3Service struct {
	config             *config.Config
	logger             *zap.Logger
	multiChainManager  *web3.MultiChainManager
	signatureVerifier  *web3.SignatureVerifier
	walletManager      *web3.WalletManager
	nftVerifier        *web3.NFTVerifier
	gasMonitor         *web3.GasMonitor
	ipfsClient         *web3.IPFSClient
	contractInteractor *web3.ContractInteractor
	transactionQueue   *web3.TransactionQueue
}

// NewWeb3Service creates a new Web3 service
func NewWeb3Service(cfg *config.Config, logger *zap.Logger) (*Web3Service, error) {
	logger.Info("Initializing Web3 service")

	service := &Web3Service{
		config:            cfg,
		logger:            logger,
		multiChainManager: web3.NewMultiChainManager(logger),
		signatureVerifier: web3.NewSignatureVerifier(logger),
		walletManager:     web3.NewWalletManager(logger),
		transactionQueue:  web3.NewTransactionQueue(1000),
	}

	// Initialize primary chain (Ethereum)
	if err := service.multiChainManager.AddChain(11155111); err != nil {
		logger.Warn("Failed to add Ethereum Sepolia", zap.Error(err))
	}

	// Initialize Polygon testnet
	if err := service.multiChainManager.AddChain(80001); err != nil {
		logger.Warn("Failed to add Polygon Mumbai", zap.Error(err))
	}

	logger.Info("Web3 service initialized")
	return service, nil
}

// GetMultiChainManager returns the multi-chain manager
func (ws *Web3Service) GetMultiChainManager() *web3.MultiChainManager {
	return ws.multiChainManager
}

// GetSignatureVerifier returns the signature verifier
func (ws *Web3Service) GetSignatureVerifier() *web3.SignatureVerifier {
	return ws.signatureVerifier
}

// GetWalletManager returns the wallet manager
func (ws *Web3Service) GetWalletManager() *web3.WalletManager {
	return ws.walletManager
}

// GetNFTVerifier returns the NFT verifier
func (ws *Web3Service) GetNFTVerifier() *web3.NFTVerifier {
	return ws.nftVerifier
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

// VerifySignature verifies a message signature
func (ws *Web3Service) VerifySignature(ctx context.Context, address string, message string, signature string) (bool, error) {
	ws.logger.Debug("Verifying signature", zap.String("address", address))
	return ws.signatureVerifier.VerifySignature(address, message, signature)
}

// VerifyNFTOwnership verifies NFT ownership
func (ws *Web3Service) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress string, tokenID string, ownerAddress string) (bool, error) {
	ws.logger.Debug("Verifying NFT ownership",
		zap.Int64("chain_id", chainID),
		zap.String("contract", contractAddress),
		zap.String("token_id", tokenID),
		zap.String("owner", ownerAddress))

	// Get chain client
	client, err := ws.multiChainManager.GetClient(chainID)
	if err != nil {
		return false, err
	}

	// Create NFT verifier
	nftVerifier := web3.NewNFTVerifier(client.GetEthClient(), ws.logger)

	// Verify ownership
	return nftVerifier.VerifyNFTOwnership(ctx, contractAddress, tokenID, ownerAddress)
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

	return ws.gasMonitor.GetGasPriceLevels(), nil
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

// GetTestnetChains gets all testnet chains
func (ws *Web3Service) GetTestnetChains() []*web3.ChainConfig {
	return ws.multiChainManager.GetTestnetChains()
}

// GetMainnetChains gets all mainnet chains
func (ws *Web3Service) GetMainnetChains() []*web3.ChainConfig {
	return ws.multiChainManager.GetMainnetChains()
}

// GetBalance gets the balance for an address
func (ws *Web3Service) GetBalance(ctx context.Context, address string) (int64, error) {
	ws.logger.Debug("Getting balance", zap.String("address", address))

	return 0, fmt.Errorf("balance retrieval not implemented")
}

// CreateNFT creates a new NFT
func (ws *Web3Service) CreateNFT(ctx context.Context, nft interface{}) error {
	ws.logger.Debug("Creating NFT")

	return fmt.Errorf("NFT creation not implemented")
}

// GetNFT gets an NFT by ID
func (ws *Web3Service) GetNFT(ctx context.Context, id string) (interface{}, error) {
	ws.logger.Debug("Getting NFT", zap.String("id", id))

	return nil, fmt.Errorf("NFT retrieval not implemented")
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

// Close closes the Web3 service
func (ws *Web3Service) Close() {
	ws.logger.Info("Closing Web3 service")

	if ws.multiChainManager != nil {
		ws.multiChainManager.Close()
	}

	ws.logger.Info("Web3 service closed")
}
