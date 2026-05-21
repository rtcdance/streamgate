package web3

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rtcdance/streamgate/pkg/web3/event"
	"go.uber.org/zap"
)

// SendTransaction sends a signed transaction through the current RPC endpoint.
// Unlike read operations, it does NOT failover — sending the same signed tx to
// multiple RPCs risks duplicate submission. The caller should handle retries by
// building a new tx with a fresh nonce.
// If the RPC returns an error, SendTransaction checks whether the tx is already
// pending (it may have been accepted despite the error response) to prevent
// the caller from accidentally resubmitting with a conflicting nonce.
func (cc *ChainClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	if cc.closed.Load() {
		return fmt.Errorf("chain client closed")
	}
	cc.wg.Add(1)
	defer cc.wg.Done()

	client := cc.client.Load()
	if client == nil {
		if err := cc.connectAny(); err != nil {
			return fmt.Errorf("sendtx: no rpc available: %w", err)
		}
		client = cc.client.Load()
	}

	if err := client.SendTransaction(ctx, tx); err != nil {
		cc.logger.Warn("SendTransaction failed on RPC",
			zap.String("rpc_url", cc.rpcURL),
			zap.Error(err))

		if alreadyPending, checkErr := cc.isTxPending(ctx, tx.Hash()); checkErr == nil && alreadyPending {
			cc.logger.Warn("Transaction is already pending despite RPC error — likely accepted",
				zap.String("tx_hash", tx.Hash().Hex()))
			return nil
		}

		return fmt.Errorf("sendtx failed on %s: %w", cc.rpcURL, err)
	}

	cc.logger.Info("Transaction sent",
		zap.String("tx_hash", tx.Hash().Hex()),
		zap.String("rpc_url", cc.rpcURL))
	return nil
}

func (cc *ChainClient) isTxPending(ctx context.Context, txHash common.Hash) (bool, error) {
	client := cc.client.Load()
	if client == nil {
		return false, fmt.Errorf("no client available")
	}
	_, isPending, err := client.TransactionByHash(ctx, txHash)
	if err != nil {
		return false, err
	}
	return isPending, nil
}

// ParseReceiptEvents populates the Events field of a ReceiptInfo by decoding
// the raw receipt logs using the provided EventParser. It fetches the raw
// receipt from the chain to access the full log data.
func (cc *ChainClient) ParseReceiptEvents(ctx context.Context, receipt *ReceiptInfo, parser *event.EventParser) error {
	hash := common.HexToHash(receipt.TransactionHash)
	rawReceipt, err := withChainClient(ctx, cc, "TransactionReceipt", func(client *ethclient.Client) (*types.Receipt, error) {
		return client.TransactionReceipt(ctx, hash)
	})
	if err != nil {
		return fmt.Errorf("failed to fetch receipt for event parsing: %w", err)
	}
	receipt.Events = parser.ParseLogs(rawReceipt.Logs)
	return nil
}

func (cc *ChainClient) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return withChainClient(ctx, cc, "TransactionReceipt", func(client *ethclient.Client) (*types.Receipt, error) {
		return client.TransactionReceipt(ctx, hash)
	})
}
