package signature

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type accountChainStub struct {
	chainID    *big.Int
	code       []byte
	result     []byte
	fail       string
	changed    bool
	reads      int
	calls      int
	call       ethereum.CallMsg
	number     *big.Int
	codeNumber *big.Int
	wait       bool
}

func newAccountChainStub() *accountChainStub {
	return &accountChainStub{chainID: big.NewInt(31337), code: []byte{0x60, 0x00},
		result: append(append([]byte{}, EIP1271MagicValue[:]...), make([]byte, 28)...)}
}

func (s *accountChainStub) ChainID(context.Context) (*big.Int, error) {
	s.reads++
	if s.fail == "chain" {
		return nil, errors.New("private RPC URL must not escape")
	}
	return s.chainID, nil
}

func (s *accountChainStub) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	s.reads++
	if s.fail == "header" {
		return nil, errors.New("safe unsupported")
	}
	if number == nil {
		panic("latest header forbidden")
	}
	if number.Sign() < 0 && number.Int64() != int64(rpc.SafeBlockNumber) {
		panic("wrong tag")
	}
	header := &types.Header{Number: big.NewInt(17), Extra: []byte("original")}
	if s.changed && number.Sign() >= 0 {
		header.Extra = []byte("reorg")
	}
	return header, nil
}

func (s *accountChainStub) CodeAt(_ context.Context, _ common.Address, number *big.Int) ([]byte, error) {
	s.reads++
	if number == nil {
		panic("latest code forbidden")
	}
	s.codeNumber = new(big.Int).Set(number)
	if s.fail == "code" {
		return nil, errors.New("unavailable is not empty code")
	}
	return s.code, nil
}

func (s *accountChainStub) CallContract(ctx context.Context, call ethereum.CallMsg, number *big.Int) ([]byte, error) {
	s.calls++
	s.call = call
	if number == nil {
		panic("latest call forbidden")
	}
	s.number = new(big.Int).Set(number)
	if s.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.fail == "revert" {
		return nil, errors.New("execution reverted: policy changed")
	}
	if s.fail == "call" {
		return nil, errors.New("https://private-rpc.invalid/secret")
	}
	return s.result, nil
}

const accountTestAddress = "0x1234567890123456789012345678901234567890"

func TestAccountVerifier_OpaqueContractProofPinned(t *testing.T) {
	chain := newAccountChainStub()
	proof, err := NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), 31337, "siwe", accountTestAddress, "message", "0x01020304050607")
	require.NoError(t, err)
	require.Equal(t, AccountContract, proof.Kind)
	require.Equal(t, common.HexToAddress(accountTestAddress), proof.Address)
	require.Equal(t, int64(31337), proof.ChainID)
	require.Equal(t, uint64(17), proof.BlockNumber)
	require.Equal(t, chain.codeNumber, chain.number)
	require.Equal(t, uint64(WalletCallGasLimit), chain.call.Gas)
	require.NotZero(t, proof.ObservedAt)
	require.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7}, proof.Signature)
	args, err := parseEIP1271ABI().Methods["isValidSignature"].Inputs.Unpack(chain.call.Data[4:])
	require.NoError(t, err)
	require.Equal(t, [32]byte(proof.Digest), args[0])
	require.Equal(t, proof.Signature, args[1])
	// The EIP-191 hash includes the UTF-8 byte length, not rune count.
	message := "登录"
	proof, err = NewAccountVerifier(newAccountChainStub(), zap.NewNop()).Verify(context.Background(), 31337, "siwe", accountTestAddress, message, "0x01")
	require.NoError(t, err)
	require.Equal(t, common.BytesToHash(NewSignatureVerifier(zap.NewNop()).hashMessage(message)), proof.Digest)
}

func TestAccountVerifier_RejectsBeforeRPC(t *testing.T) {
	tests := []struct {
		name, address, scheme, signature string
		chainID                          int64
		want                             error
	}{
		{"invalid address", "bad", "siwe", "0x01", 31337, ErrWalletFormat},
		{"zero chain", accountTestAddress, "siwe", "0x01", 0, ErrWalletFormat},
		{"odd hex", accountTestAddress, "siwe", "0x123", 31337, ErrWalletFormat},
		{"invalid hex", accountTestAddress, "siwe", "0xzz", 31337, ErrWalletFormat},
		{"empty hex", accountTestAddress, "siwe", "0x", 31337, ErrWalletFormat},
		{"over limit", accountTestAddress, "siwe", "0x" + strings.Repeat("aa", MaxWalletSignatureBytes+1), 31337, ErrWalletFormat},
		{"6492", accountTestAddress, "siwe", "0x01" + strings.Repeat("6492", 16), 31337, ErrWalletUnsupported},
		{"unknown scheme", accountTestAddress, "other", "0x01", 31337, ErrWalletUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := newAccountChainStub()
			_, err := NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), tt.chainID, tt.scheme, tt.address, "message", tt.signature)
			require.ErrorIs(t, err, tt.want)
			require.Zero(t, chain.reads)
			require.Zero(t, chain.calls)
		})
	}
}

func TestAccountVerifier_FailClosed(t *testing.T) {
	tests := []struct {
		name   string
		change func(*accountChainStub)
		want   error
	}{
		{"network read", func(s *accountChainStub) { s.fail = "chain" }, ErrWalletDependency},
		{"wrong chain", func(s *accountChainStub) { s.chainID = big.NewInt(1) }, ErrWalletDependency},
		{"missing safe", func(s *accountChainStub) { s.fail = "header" }, ErrWalletDependency},
		{"code outage", func(s *accountChainStub) { s.fail = "code" }, ErrWalletDependency},
		{"call outage", func(s *accountChainStub) { s.fail = "call" }, ErrWalletDependency},
		{"revert", func(s *accountChainStub) { s.fail = "revert" }, ErrWalletSignature},
		{"7702", func(s *accountChainStub) { s.code = append([]byte{0xef, 0x01, 0x00}, make([]byte, 20)...) }, ErrWalletUnsupported},
		{"reorg", func(s *accountChainStub) { s.changed = true }, ErrWalletDependency},
		{"wrong magic", func(s *accountChainStub) { s.result[0] = 0xff }, ErrWalletSignature},
		{"empty", func(s *accountChainStub) { s.result = nil }, ErrWalletSignature},
		{"raw four bytes", func(s *accountChainStub) { s.result = append([]byte{}, EIP1271MagicValue[:]...) }, ErrWalletSignature},
		{"dirty padding", func(s *accountChainStub) { s.result[31] = 1 }, ErrWalletSignature},
		{"extra word", func(s *accountChainStub) { s.result = append(s.result, make([]byte, 32)...) }, ErrWalletSignature},
		{"embedded magic", func(s *accountChainStub) { s.result = append(make([]byte, 28), EIP1271MagicValue[:]...) }, ErrWalletSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := newAccountChainStub()
			tt.change(chain)
			proof, err := NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), 31337, "siwe", accountTestAddress, "message", "0x01")
			require.Nil(t, proof)
			require.ErrorIs(t, err, tt.want)
			require.NotContains(t, err.Error(), "private-rpc")
		})
	}
}

func TestAccountVerifier_NoContractEOAFallback(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signer := NewSignatureVerifier(zap.NewNop())
	encoded, err := signer.SignMessage("message", key)
	require.NoError(t, err)
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	chain := newAccountChainStub()
	chain.result = make([]byte, 32)
	_, err = NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), 31337, "siwe", address, "message", encoded)
	require.ErrorIs(t, err, ErrWalletSignature)
	require.Equal(t, 1, chain.calls, "on-chain policy must override a matching ECDSA recovery")
	chain = newAccountChainStub()
	chain.code = nil
	proof, err := NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), 31337, "siwe", address, "message", encoded)
	require.NoError(t, err)
	require.Equal(t, AccountEOA, proof.Kind)
	require.Zero(t, chain.calls)
}

func TestAccountVerifier_BudgetsAndUnsupportedTypedContract(t *testing.T) {
	chain := newAccountChainStub()
	_, err := NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), 31337, "eip712", accountTestAddress, "{}", "0x01")
	require.ErrorIs(t, err, ErrWalletUnsupported)
	require.Zero(t, chain.calls)
	chain = newAccountChainStub()
	v := NewAccountVerifier(chain, zap.NewNop())
	start := time.Now()
	read := 0
	v.now = func() time.Time {
		read++
		if read > 1 {
			return start.Add(6 * time.Second)
		}
		return start
	}
	_, err = v.Verify(context.Background(), 31337, "siwe", accountTestAddress, "message", "0x01")
	require.ErrorIs(t, err, ErrWalletDependency)
	chain = newAccountChainStub()
	chain.wait = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = NewAccountVerifier(chain, zap.NewNop()).Verify(ctx, 31337, "siwe", accountTestAddress, "message", "0x01")
	require.ErrorIs(t, err, ErrWalletDependency)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestEIP1271_PinnedInputAndLegacyDispatch(t *testing.T) {
	chain := newAccountChainStub()
	checker := NewEIP1271Checker(chain, zap.NewNop())
	_, err := checker.IsValidSignatureAt(context.Background(), accountTestAddress, [32]byte{}, []byte{1}, nil)
	require.ErrorIs(t, err, ErrWalletFormat)
	require.Zero(t, chain.calls)
	v := NewSignatureVerifier(zap.NewNop())
	v.SetEIP1271Checker(checker)
	valid, err := v.VerifySignature(context.Background(), accountTestAddress, "message", "0x0102")
	require.NoError(t, err)
	require.True(t, valid)
	_, err = checker.IsValidSignatureAt(context.Background(), accountTestAddress, [32]byte{}, bytes.Repeat([]byte{1}, MaxWalletSignatureBytes+1), big.NewInt(17))
	require.ErrorIs(t, err, ErrWalletFormat)
}

func TestAccountVerifier_EIP712EOAChainAndDigest(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	typed := testPermitTypedData()
	typed.Domain.ChainId = big.NewInt(31337)
	signer := NewEIP712Verifier(zap.NewNop())
	encoded, err := signer.SignTypedData(typed, key)
	require.NoError(t, err)
	message, err := json.Marshal(typed)
	require.NoError(t, err)
	chain := newAccountChainStub()
	chain.code = nil
	proof, err := NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), 31337, "eip712", crypto.PubkeyToAddress(key.PublicKey).Hex(), string(message), encoded)
	require.NoError(t, err)
	require.Equal(t, AccountEOA, proof.Kind)
	expected, err := hashTypedData(signer.convertToAPITypes(typed))
	require.NoError(t, err)
	require.Equal(t, common.BytesToHash(expected), proof.Digest)
	typed.Domain.ChainId = big.NewInt(1)
	message, err = json.Marshal(typed)
	require.NoError(t, err)
	_, err = NewAccountVerifier(chain, zap.NewNop()).Verify(context.Background(), 31337, "eip712", crypto.PubkeyToAddress(key.PublicKey).Hex(), string(message), encoded)
	require.ErrorIs(t, err, ErrWalletFormat)
}

func TestAccountProof_OriginalObservationAge(t *testing.T) {
	now := time.Now()
	proof := &AccountProof{ObservedAt: now}
	require.True(t, proof.Fresh(now.Add(MaxWalletProofAge)))
	require.False(t, proof.Fresh(now.Add(MaxWalletProofAge+time.Nanosecond)))
	require.False(t, proof.Fresh(now.Add(-time.Nanosecond)))
	var missing *AccountProof
	require.False(t, missing.Fresh(now))
}

func TestAccountVerifier_ConfiguredBudgetsReachCalls(t *testing.T) {
	chain := newAccountChainStub()
	budget := VerificationBudget{MaxSignatureBytes: 2, CallGasLimit: 100000, Timeout: time.Second, ProofMaxAge: time.Second}
	v := NewAccountVerifier(chain, zap.NewNop(), budget)
	_, err := v.Verify(t.Context(), 31337, "siwe", accountTestAddress, "message", "0x0102")
	require.NoError(t, err)
	require.Equal(t, uint64(100000), chain.call.Gas)
	chain = newAccountChainStub()
	v = NewAccountVerifier(chain, zap.NewNop(), budget)
	_, err = v.Verify(t.Context(), 31337, "siwe", accountTestAddress, "message", "0x010203")
	require.ErrorIs(t, err, ErrWalletFormat)
	require.Zero(t, chain.reads)
	chain = newAccountChainStub()
	chain.wait = true
	budget.Timeout = 10 * time.Millisecond
	_, err = NewAccountVerifier(chain, zap.NewNop(), budget).Verify(t.Context(), 31337, "siwe", accountTestAddress, "message", "0x01")
	require.ErrorIs(t, err, ErrWalletDependency)
	budget.Timeout = 6 * time.Second
	chain = newAccountChainStub()
	_, err = NewAccountVerifier(chain, zap.NewNop(), budget).Verify(t.Context(), 31337, "siwe", accountTestAddress, "message", "0x01")
	require.ErrorIs(t, err, ErrWalletFormat)
	require.Zero(t, chain.reads)
}
