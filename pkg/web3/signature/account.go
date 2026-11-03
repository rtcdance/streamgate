package signature

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

const (
	AccountEOA                = "eoa"
	AccountContract           = "erc1271"
	MaxWalletSignatureBytes   = 8192
	WalletVerificationTimeout = 5 * time.Second
	MaxWalletProofAge         = 5 * time.Second
	WalletCallGasLimit        = 500000
)

var (
	ErrWalletFormat      = errors.New("malformed wallet proof")
	ErrWalletUnsupported = errors.New("unsupported wallet or signing scheme")
	ErrWalletDependency  = errors.New("wallet verification dependency unavailable")
	ErrWalletSignature   = errors.New("wallet signature rejected")
	wrapped6492Suffix    = bytes.Repeat([]byte{0x64, 0x92}, 16)
)

// VerificationBudget is an immutable per-verifier tightening of hard limits.
type VerificationBudget struct {
	MaxSignatureBytes int
	CallGasLimit      uint64
	Timeout           time.Duration
	ProofMaxAge       time.Duration
}

func (b VerificationBudget) normalized() (VerificationBudget, error) {
	if b.MaxSignatureBytes == 0 {
		b.MaxSignatureBytes = MaxWalletSignatureBytes
	}
	if b.CallGasLimit == 0 {
		b.CallGasLimit = WalletCallGasLimit
	}
	if b.Timeout == 0 {
		b.Timeout = WalletVerificationTimeout
	}
	if b.ProofMaxAge == 0 {
		b.ProofMaxAge = MaxWalletProofAge
	}
	if b.MaxSignatureBytes < 0 || b.MaxSignatureBytes > MaxWalletSignatureBytes || b.CallGasLimit > WalletCallGasLimit || b.Timeout < 0 || b.Timeout > WalletVerificationTimeout || b.ProofMaxAge < 0 || b.ProofMaxAge > MaxWalletProofAge {
		return b, ErrWalletFormat
	}
	return b, nil
}

// AccountChain is a server-selected, chain-aware RPC client. Every read in a
// proof uses one concrete safe anchor; it must never substitute latest.
type AccountChain interface {
	ContractCaller
	ChainID(context.Context) (*big.Int, error)
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
	CodeAt(context.Context, common.Address, *big.Int) ([]byte, error)
}

// AccountProof is the observation used to admit or renew a wallet session.
// It is not a bearer credential. Signature bytes stay out of JSON/log output.
type AccountProof struct {
	Address     common.Address `json:"address"`
	ChainID     int64          `json:"chain_id"`
	Kind        string         `json:"kind"`
	Digest      common.Hash    `json:"digest"`
	CodeHash    common.Hash    `json:"code_hash"`
	BlockNumber uint64         `json:"block_number"`
	BlockHash   common.Hash    `json:"block_hash"`
	ObservedAt  time.Time      `json:"observed_at"`
	Signature   []byte         `json:"-"`
}

// AccountVerifier dispatches by on-chain account type before EOA decoding.
// No mutable chain/checker is shared between concurrent verification requests.
type AccountVerifier struct {
	chain   AccountChain
	logger  *zap.Logger
	now     func() time.Time
	budget  VerificationBudget
	initErr error
}

func NewAccountVerifier(chain AccountChain, logger *zap.Logger, budgets ...VerificationBudget) *AccountVerifier {
	if logger == nil {
		logger = zap.NewNop()
	}
	var budget VerificationBudget
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	budget, err := budget.normalized()
	return &AccountVerifier{chain: chain, logger: logger, now: time.Now, budget: budget, initErr: err}
}

// DecodeWalletSignature rejects invalid/oversized hex before allocating or RPC.
func DecodeWalletSignature(encoded string) ([]byte, error) {
	raw := strings.TrimPrefix(encoded, "0x")
	if raw == "" || len(raw)%2 != 0 || len(raw) > 2*MaxWalletSignatureBytes {
		return nil, ErrWalletFormat
	}
	sig, err := hex.DecodeString(raw)
	if err != nil {
		return nil, ErrWalletFormat
	}
	if bytes.HasSuffix(sig, wrapped6492Suffix) {
		return nil, ErrWalletUnsupported
	}
	return sig, nil
}

// Verify validates an admitted message. Challenge, SIWE origin and nonce checks
// belong to the auth service and must run before calling this method.
func (v *AccountVerifier) Verify(ctx context.Context, chainID int64, scheme, address, message, encoded string) (*AccountProof, error) {
	if v.initErr != nil {
		return nil, v.initErr
	}
	if chainID <= 0 || !common.IsHexAddress(address) {
		return nil, ErrWalletFormat
	}
	sig, err := DecodeWalletSignature(encoded)
	if err != nil {
		return nil, err
	}
	if len(sig) > v.budget.MaxSignatureBytes {
		return nil, ErrWalletFormat
	}
	if scheme != "siwe" && scheme != "personal_sign" && scheme != "eip712" {
		return nil, ErrWalletUnsupported
	}
	if v.chain == nil {
		return nil, ErrWalletDependency
	}
	ctx, cancel := context.WithTimeout(ctx, v.budget.Timeout)
	defer cancel()
	observedAt := v.now().UTC()
	id, err := v.chain.ChainID(ctx)
	if err != nil || id == nil || id.Cmp(big.NewInt(chainID)) != 0 {
		return nil, fmt.Errorf("%w: chain identity", ErrWalletDependency)
	}
	anchor, err := v.chain.HeaderByNumber(ctx, big.NewInt(int64(rpc.SafeBlockNumber)))
	if err != nil || anchor == nil || anchor.Number == nil || !anchor.Number.IsUint64() {
		return nil, fmt.Errorf("%w: safe anchor", ErrWalletDependency)
	}
	number := new(big.Int).Set(anchor.Number)
	wallet := common.HexToAddress(address)
	code, err := v.chain.CodeAt(ctx, wallet, number)
	if err != nil {
		return nil, fmt.Errorf("%w: account code", ErrWalletDependency)
	}
	// Delegated EOAs are a separate protocol, not ordinary ERC-1271 wallets.
	if bytes.HasPrefix(code, []byte{0xef, 0x01, 0x00}) {
		return nil, ErrWalletUnsupported
	}
	proof := &AccountProof{Address: wallet, ChainID: chainID, Kind: AccountEOA,
		Digest:   crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message))),
		CodeHash: crypto.Keccak256Hash(code), BlockNumber: number.Uint64(), BlockHash: anchor.Hash(),
		ObservedAt: observedAt, Signature: append([]byte(nil), sig...)}
	if len(code) != 0 {
		proof.Kind = AccountContract
		if scheme == "eip712" {
			return nil, ErrWalletUnsupported
		}
		valid, callErr := NewEIP1271Checker(v.chain, v.logger).isValidSignatureAt(ctx, address, proof.Digest, sig, number, v.budget.CallGasLimit)
		if callErr != nil {
			return nil, callErr
		}
		if !valid {
			return nil, ErrWalletSignature
		}
	} else {
		digest, verifyErr := v.verifyEOA(ctx, chainID, scheme, address, message, encoded)
		if verifyErr != nil {
			return nil, verifyErr
		}
		proof.Digest = digest
	}
	// Re-read the exact height after all reads (including RPC failover). A
	// number/hash mismatch invalidates the observation instead of reanchoring it.
	recheck, err := v.chain.HeaderByNumber(ctx, number)
	if err != nil || recheck == nil || recheck.Number == nil || recheck.Number.Cmp(number) != 0 || recheck.Hash() != proof.BlockHash {
		return nil, fmt.Errorf("%w: anchor changed", ErrWalletDependency)
	}
	if ctx.Err() != nil || v.now().Before(observedAt) || v.now().Sub(observedAt) > v.budget.ProofMaxAge {
		return nil, fmt.Errorf("%w: expired observation", ErrWalletDependency)
	}
	return proof, nil
}

func (v *AccountVerifier) verifyEOA(ctx context.Context, chainID int64, scheme, address, message, encoded string) (common.Hash, error) {
	var valid bool
	var err error
	var digest common.Hash
	if scheme == "eip712" {
		var typed EIP712TypedData
		if json.Unmarshal([]byte(message), &typed) != nil || typed.Domain.ChainId == nil || typed.Domain.ChainId.Cmp(big.NewInt(chainID)) != 0 {
			return digest, ErrWalletFormat
		}
		verifier := NewEIP712Verifier(v.logger)
		valid, err = verifier.VerifyTypedData(address, &typed, encoded)
		if err == nil && valid {
			var hash []byte
			hash, err = hashTypedData(verifier.convertToAPITypes(&typed))
			digest = common.BytesToHash(hash)
		}
	} else {
		verifier := NewSignatureVerifier(v.logger)
		valid, err = verifier.VerifySignature(ctx, address, message, encoded)
		digest = common.BytesToHash(verifier.hashMessage(message))
	}
	if err != nil || !valid {
		return digest, ErrWalletSignature
	}
	return digest, nil
}

// Fresh is checked again at issuance after any storage or concurrency wait.
func (p *AccountProof) Fresh(at time.Time) bool {
	return p != nil && !p.ObservedAt.IsZero() && !at.Before(p.ObservedAt) && at.Sub(p.ObservedAt) <= MaxWalletProofAge
}

func isWalletRevert(err error) bool {
	var coded rpc.Error
	return (errors.As(err, &coded) && coded.ErrorCode() == 3) || strings.Contains(strings.ToLower(err.Error()), "execution reverted")
}

func walletCall(ctx context.Context, caller ContractCaller, call ethereum.CallMsg, number *big.Int) ([]byte, error) {
	result, err := caller.CallContract(ctx, call, number)
	if err != nil {
		if isWalletRevert(err) {
			return nil, ErrWalletSignature
		}
		return nil, ErrWalletDependency
	}
	return result, nil
}
