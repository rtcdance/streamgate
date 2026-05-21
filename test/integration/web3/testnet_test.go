//go:build testnet

package web3_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/web3"
	"go.uber.org/zap"
)

// These tests require a real Sepolia RPC endpoint.
// Run with: go test -tags=testnet ./test/integration/web3/
//
// Environment variables:
//   SEPOLIA_RPC — Sepolia RPC URL (e.g. https://sepolia.infura.io/v3/YOUR_KEY)

func getSepoliaRPC(t *testing.T) string {
	t.Helper()
	rpc := os.Getenv("SEPOLIA_RPC")
	if rpc == "" {
		t.Skip("SEPOLIA_RPC not set, skipping testnet test")
	}
	return rpc
}

func TestSepolia_BlockNumber(t *testing.T) {
	rpcURL := getSepoliaRPC(t)
	client, err := web3.NewChainClient(rpcURL, 11155111, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to connect to Sepolia: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	blockNum, err := client.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("failed to get block number: %v", err)
	}
	t.Logf("Sepolia block number: %d", blockNum)

	if blockNum == 0 {
		t.Fatal("expected non-zero block number")
	}
}

func TestSepolia_GetBalance(t *testing.T) {
	rpcURL := getSepoliaRPC(t)
	client, err := web3.NewChainClient(rpcURL, 11155111, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to connect to Sepolia: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Vitalik's address — should have some ETH even on Sepolia
	balance, err := client.GetBalance(ctx, "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}
	t.Logf("Sepolia balance: %s wei", balance.String())
}

func TestSepolia_HealthCheck(t *testing.T) {
	rpcURL := getSepoliaRPC(t)
	client, err := web3.NewChainClient(rpcURL, 11155111, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to connect to Sepolia: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}
