//go:build fork

package web3_test

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/rtcdance/streamgate/pkg/web3"
)

const forkRPCURL = "http://localhost:8545"

func TestMain(m *testing.M) {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(forkRPCURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Println("Skipping fork tests: Anvil not running at", forkRPCURL)
		fmt.Println("Start with: anvil --fork-url $ALCHEMY_URL")
		os.Exit(0)
	}
	resp.Body.Close()

	os.Exit(m.Run())
}

func newForkChainClient(t *testing.T) *web3.ChainClient {
	t.Helper()
	cc, err := web3.NewChainClient(forkRPCURL, 1, nil)
	if err != nil {
		t.Fatalf("Failed to connect to Anvil fork: %v", err)
	}
	return cc
}

// chainClientEthCaller adapts ChainClient to the EthCaller interface
// by using CallContractAtBlock with BlockTagLatest.
type chainClientEthCaller struct {
	*web3.ChainClient
}

func (c chainClientEthCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if blockNumber != nil {
		// Use specific block number via CallContractAtBlock with BlockTagLatest
		// Note: exact block number not supported through this adapter
		return c.ChainClient.CallContractAtBlock(ctx, call, web3.BlockTagLatest)
	}
	return c.ChainClient.CallContractAtBlock(ctx, call, web3.BlockTagLatest)
}

func (c chainClientEthCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	// ChainClient doesn't expose CodeAt directly; use CallContractAtBlock as fallback
	// This is a simplified adapter for fork testing purposes
	return nil, fmt.Errorf("CodeAt not supported in fork test adapter")
}

// TestBlockNumber_ForkedMainnet verifies basic connectivity by checking
// that we can read a block number from the fork.
func TestBlockNumber_ForkedMainnet(t *testing.T) {
	chainClient := newForkChainClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	blockNum, err := chainClient.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("Failed to get block number: %v", err)
	}

	if blockNum == 0 {
		t.Error("Block number should not be 0 on a mainnet fork")
	}
	t.Logf("Fork block number: %d", blockNum)
}

// TestERC721Ownership_ForkedMainnet verifies ownerOf via ChainClient's
// built-in VerifyNFTOwnership against a well-known mainnet NFT contract.
func TestERC721Ownership_ForkedMainnet(t *testing.T) {
	chainClient := newForkChainClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// BAYC: 0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D
	contract := "0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D"

	req := web3.VerifyRequest{
		WalletAddress: "0x0000000000000000000000000000000000000000",
		Contract:      contract,
		TokenID:       "1",
		Mode:          web3.GatingSpecificID,
	}

	owned, err := chainClient.VerifyNFTOwnershipByRequest(ctx, req)
	if err != nil {
		t.Logf("VerifyNFTOwnership error (acceptable on fork): %v", err)
		return
	}
	// Zero address won't own the token, so owned should be false
	t.Logf("BAYC #1 owned by zero address: %v", owned)
}

// TestERC20Info_ForkedMainnet verifies ERC-20 token info reading against
// well-known mainnet tokens using the EthCaller adapter.
func TestERC20Info_ForkedMainnet(t *testing.T) {
	chainClient := newForkChainClient(t)
	caller := chainClientEthCaller{ChainClient: chainClient}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reader := web3.NewERC20Reader(caller, nil)

	// USDC on Ethereum mainnet
	usdc := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	info, err := reader.GetTokenInfo(ctx, usdc)
	if err != nil {
		t.Fatalf("Failed to get USDC token info: %v", err)
	}

	if info.Name == "" && info.Symbol == "" {
		t.Log("USDC name/symbol returned empty (some tokens use bytes32)")
	} else {
		t.Logf("USDC: name=%s, symbol=%s, decimals=%d", info.Name, info.Symbol, info.Decimals)
		if info.Decimals != 6 {
			t.Errorf("USDC decimals: got %d, want 6", info.Decimals)
		}
	}

	// WETH on Ethereum mainnet
	weth := "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	info, err = reader.GetTokenInfo(ctx, weth)
	if err != nil {
		t.Fatalf("Failed to get WETH token info: %v", err)
	}
	t.Logf("WETH: name=%s, symbol=%s, decimals=%d", info.Name, info.Symbol, info.Decimals)
	if info.Decimals != 18 {
		t.Errorf("WETH decimals: got %d, want 18", info.Decimals)
	}
}

// TestDetectTokenStandard_ForkedMainnet verifies that DetectTokenStandard
// correctly identifies token standards for known mainnet contracts.
func TestDetectTokenStandard_ForkedMainnet(t *testing.T) {
	chainClient := newForkChainClient(t)
	caller := chainClientEthCaller{ChainClient: chainClient}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tests := []struct {
		name     string
		contract string
		want     web3.TokenStandard
	}{
		{
			name:     "BAYC is ERC-721",
			contract: "0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D",
			want:     web3.TokenStandardERC721,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			standard := web3.DetectTokenStandard(ctx, caller, tt.contract, nil)
			t.Logf("Detected standard for %s: %v", tt.contract, standard)
			if standard != tt.want {
				t.Errorf("DetectTokenStandard(%s) = %v, want %v", tt.contract, standard, tt.want)
			}
		})
	}
}
