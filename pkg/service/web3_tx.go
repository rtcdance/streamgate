package service

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"go.uber.org/zap"
)

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

// SubmitPermit submits an EIP-2612 permit transaction to an ERC-20 contract.
// The caller provides the signed permit parameters (v, r, s from EIP-712 signing).
// This completes the gasless approval flow: sign off-chain to submit on-chain.
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

// VerifySolanaMetaplexMetadata verifies Metaplex NFT ownership.
func (ws *Web3Service) VerifySolanaMetaplexNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error) {
	ws.logger.Debug("Verifying Solana Metaplex NFT ownership",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress))

	if ws.solanaVerifier == nil {
		return false, fmt.Errorf("solana verifier not initialized")
	}
	return ws.solanaVerifier.VerifyMetaplexNFTOwnership(ctx, mintAddress, ownerAddress)
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
