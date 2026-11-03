package service

import (
	"errors"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"go.uber.org/zap"
)

func initializeWeb3Chains(cfg *config.Config, manager web3.ChainManagerInterface, logger *zap.Logger) error {
	// Restricted profiles cannot inherit the process-global built-in chain
	// catalog. Adding defaults here would dial Anvil/Polygon/Solana despite
	// the validated Sepolia-only configuration, or give the worker RPC access.
	if cfg.Environment == "w3sepolia-worker" {
		return errors.New("w3sepolia-worker must not initialize a Web3 service")
	}
	chains := []int64{11155111, 80002, 31337, -1, -2}
	if cfg.Environment == "w3sepolia" {
		if cfg.Web3.ChainID != 11155111 || len(cfg.Web3.Chains) != 1 || cfg.Web3.Chains[0].ID != 11155111 {
			return errors.New("w3sepolia service requires only configured chain 11155111")
		}
		chains = []int64{11155111}
	}
	for _, chain := range chains {
		if err := manager.AddChain(chain); err != nil {
			if cfg.Environment == "w3sepolia" {
				return errors.New("w3sepolia chain initialization unavailable")
			}
			logger.Warn("Failed to initialize chain", zap.Int64("chain_id", chain), zap.Error(err))
		}
	}
	return nil
}
