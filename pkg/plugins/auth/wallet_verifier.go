package auth

import (
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/service/sigchain"
	"go.uber.org/zap"
)

// NewProfileSignatureVerifier wires the stateless legacy probe to shared logic.
func NewProfileSignatureVerifier(cfg *config.Config, logger *zap.Logger) (WalletSignatureVerifier, error) {
	return sigchain.NewProfileSignatureVerifier(cfg, logger)
}
