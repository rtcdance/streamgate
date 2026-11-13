package sigchain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"go.uber.org/zap"
)

// The legacy endpoint is a stateless signature probe, never a JWT issuer.
// Freeze profile/limits once and share only the concurrency bound, not a
// request-mutated chain checker. Connections exist only during a bounded call.
type ProfileWalletVerifier struct {
	chainID   int64
	rpcs      []string
	budget    signature.VerificationBudget
	bodyLimit int64
	logger    *zap.Logger
	slots     chan struct{}
}

// NewProfileSignatureVerifier supplies the legacy verification probe with the
// same account classification and limits as login. It never issues credentials.
func NewProfileSignatureVerifier(cfg *config.Config, logger *zap.Logger) (*ProfileWalletVerifier, error) {
	if cfg == nil {
		return nil, fmt.Errorf("auth configuration is required")
	}
	if err := cfg.Auth.ContractWallet.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return newProfileWalletVerifier(cfg, logger), nil
}

func newProfileWalletVerifier(cfg *config.Config, logger *zap.Logger) *ProfileWalletVerifier {
	p := cfg.Auth.ContractWallet.Effective()
	id := cfg.Web3.ChainID
	if id <= 0 {
		id = 1
	}
	var urls []string
	for _, chain := range cfg.Web3.Chains {
		if chain.ID == id {
			urls = append(urls, chain.RPCs...)
			if len(chain.RPCs) == 0 {
				urls = append(urls, chain.RPC)
			}
		}
	}
	if len(urls) == 0 {
		urls = append(urls, cfg.Web3.EthereumRPC)
	}
	seen := make(map[string]bool)
	unique := make([]string, 0, len(urls))
	for _, endpoint := range urls {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" && !seen[endpoint] {
			seen[endpoint] = true
			unique = append(unique, endpoint)
		}
	}
	return &ProfileWalletVerifier{chainID: id, rpcs: unique, logger: logger, slots: make(chan struct{}, 8), bodyLimit: p.MaxBodyBytes,
		budget: signature.VerificationBudget{MaxSignatureBytes: p.MaxSignatureBytes, CallGasLimit: uint64(p.CallGasLimit), Timeout: p.VerificationTimeout, ProofMaxAge: p.ProofMaxAge}}
}

func (v *ProfileWalletVerifier) VerifySignature(ctx context.Context, address, message, encoded string) (bool, error) {
	if !common.IsHexAddress(address) {
		return false, signature.ErrWalletFormat
	}
	decoded, err := signature.DecodeWalletSignature(encoded)
	if err != nil {
		return false, err
	}
	if len(decoded) > v.budget.MaxSignatureBytes {
		return false, signature.ErrWalletFormat
	}
	if len(v.rpcs) < 2 {
		return false, signature.ErrWalletDependency
	}
	ctx, cancel := context.WithTimeout(ctx, v.budget.Timeout)
	defer cancel()
	select {
	case v.slots <- struct{}{}:
		defer func() { <-v.slots }()
	case <-ctx.Done():
		return false, signature.ErrWalletDependency
	}
	client, err := web3.NewChainClientWithContext(ctx, v.rpcs, v.chainID, v.logger)
	if err != nil {
		return false, signature.ErrWalletDependency
	}
	defer client.Close()
	proof, err := signature.NewAccountVerifier(client, v.logger, v.budget).Verify(ctx, v.chainID, "personal_sign", address, message, encoded)
	if errors.Is(err, signature.ErrWalletSignature) {
		return false, nil
	}
	return proof != nil && err == nil, err
}

func (v *ProfileWalletVerifier) WalletAuthBodyLimit() int64 { return v.bodyLimit }
