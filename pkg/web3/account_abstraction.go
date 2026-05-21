package web3

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// IAccount defines the ERC-4337 account abstraction verification interface.
// An IAccount contract validates UserOperations (not traditional transactions).
// When integrated, the system would:
//  1. Extract the wallet's entry point address from chain config
//  2. Call IAccount.validateUserOp to verify signature and paymaster sponsorship
//  3. Use the returned validationData to decide NFT access
//
// ERC-4337 is not the only AA standard; some L2s (e.g., zkSync) have their own
// native AA. This interface provides a common entry point for future integration.
type IAccount interface {
	// ValidateUserOp simulates ERC-4337's validateUserOp call.
	// Returns the packed validation data as defined by ERC-4337:
	//   - 0x00000000...: success
	//   - 0x00000001...: signature failure
	//   - 0x00000002...: paymaster failure
	// The actual call is: IAccount(userOp.sender).validateUserOp(userOp, userOpHash, missingAccountFunds)
	ValidateUserOp(ctx context.Context, sender common.Address, userOpHash [32]byte, missingAccountFunds *big.Int, nonce *big.Int) ([]byte, error)
}

// UserOperation represents an ERC-4337 UserOperation struct.
// Fields align with the Ethereum EntryPoint contract specification.
type UserOperation struct {
	Sender               common.Address
	Nonce                *big.Int
	InitCode             []byte
	CallData             []byte
	CallGasLimit         *big.Int
	VerificationGasLimit *big.Int
	PreVerificationGas   *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	PaymasterAndData     []byte
	Signature            []byte
}

// AAProvider abstracts ERC-4337 UserOperation validation.
// The default implementation calls the sender contract's validateUserOp via
// eth_call. Future implementations may:
//   - Cache validation results per UserOpHash
//   - Batch validate operations
//   - Run validation against EntryPoint simulations
type AAProvider struct {
	client     EthCaller
	nonceCache *sync.Map // nonce -> timestamp for replay protection
	maxAge     time.Duration
}

// NewAAProvider creates an AA provider for ERC-4337 validation.
func NewAAProvider(client EthCaller) *AAProvider {
	return &AAProvider{
		client:     client,
		nonceCache: &sync.Map{},
		maxAge:     5 * time.Minute,
	}
}

// validateNonce checks if the nonce has been used recently (replay protection)
func (a *AAProvider) validateNonce(nonce *big.Int) error {
	if nonce == nil {
		return fmt.Errorf("nonce cannot be nil")
	}

	nonceKey := nonce.String()
	if ts, ok := a.nonceCache.Load(nonceKey); ok {
		if time.Since(ts.(time.Time)) < a.maxAge {
			return fmt.Errorf("nonce %s was recently used, possible replay attack", nonceKey)
		}
		// Clean up expired entry
		a.nonceCache.Delete(nonceKey)
	}

	a.nonceCache.Store(nonceKey, time.Now())
	return nil
}

// CleanupExpiredNonces removes entries older than maxAge
func (a *AAProvider) CleanupExpiredNonces() {
	now := time.Now()
	a.nonceCache.Range(func(key, value interface{}) bool {
		if now.Sub(value.(time.Time)) > a.maxAge {
			a.nonceCache.Delete(key)
		}
		return true
	})
}

// ValidateUserOp calls IAccount.validateUserOp on the sender contract.
// This is a stub: the full ABI encoding for validateUserOp should be
// generated from the ERC-4337 EntryPoint ABI when integrating.
func (a *AAProvider) ValidateUserOp(ctx context.Context, sender common.Address, userOpHash [32]byte, missingAccountFunds *big.Int, nonce *big.Int) ([]byte, error) {
	// Replay protection: check if nonce was recently used
	if err := a.validateNonce(nonce); err != nil {
		return nil, fmt.Errorf("replay protection check failed: %w", err)
	}

	return nil, fmt.Errorf("ERC-4337 validateUserOp not implemented: account abstraction validation is disabled until EntryPoint integration is complete")
}
