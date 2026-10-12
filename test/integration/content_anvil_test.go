//go:build anvil

package integration_test

// Anvil roundtrip for the ContentRegistry attestation path.
//
// Self-contained by design: it starts its own Anvil, deploys the real
// Forge-compiled ContentRegistry, and drives the production
// gateway.ProvideContentRegistry → ContentRegistryBinding path. It asserts the
// Go ABI matches the deployed contract, that the tx is accepted, that a real
// receipt with a decodable ContentRegistered log comes back, and that
// getContent returns the creator and digest that were registered.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const anvilChainID int64 = 31337

const anvilKey0 = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "go.mod not found above %s", dir)
		dir = parent
	}
}

func loadRegistryArtifact(t *testing.T) (abi.ABI, []byte) {
	t.Helper()
	p := filepath.Join(repoRoot(t), "contracts/out/ContentRegistry.sol/ContentRegistry.json")
	raw, err := os.ReadFile(p)
	require.NoError(t, err, "run `forge build` in contracts/ first")
	var art struct {
		ABI      json.RawMessage `json:"abi"`
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
	}
	require.NoError(t, json.Unmarshal(raw, &art))
	parsed, err := abi.JSON(strings.NewReader(string(art.ABI)))
	require.NoError(t, err)
	bin := common.FromHex(strings.TrimSpace(art.Bytecode.Object))
	require.NotEmpty(t, bin)
	return parsed, bin
}

func startAnvil(t *testing.T) (rpcURL string, cleanup func()) {
	t.Helper()
	anvilPath, err := exec.LookPath("anvil")
	require.NoError(t, err, "anvil not found in PATH — no skip by design")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	rpcURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, anvilPath,
		"--port", fmt.Sprint(port),
		"--chain-id", fmt.Sprint(anvilChainID),
		"--accounts", "3",
		"--slots-in-an-epoch", "1",
		"--silent",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())

	cleanup = func() {
		_ = cmd.Process.Kill()
		cancel()
		_ = cmd.Wait()
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := ethclient.Dial(rpcURL)
		if dialErr == nil {
			pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, perr := conn.BlockNumber(pctx)
			pcancel()
			conn.Close()
			if perr == nil {
				return rpcURL, cleanup
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	cleanup()
	t.Fatalf("anvil not ready within 20s at %s", rpcURL)
	return "", nil
}

func TestAnvil_ContentRegistry_RegisterAndReadBack(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	registryABI, registryBin := loadRegistryArtifact(t)

	ethClient, err := ethclient.Dial(rpcURL)
	require.NoError(t, err)
	defer ethClient.Close()

	key, err := crypto.HexToECDSA(strings.TrimPrefix(anvilKey0, "0x"))
	require.NoError(t, err)
	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(anvilChainID))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	auth.Context = ctx

	// Deploy the real contract; the deployer becomes the platform attestor
	// (onlyOwner) and is the same key the gateway provider will sign with.
	addr, deployTx, _, err := bind.DeployContract(auth, registryABI, registryBin, ethClient)
	require.NoError(t, err)
	deployReceipt, err := bind.WaitMined(ctx, ethClient, deployTx)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, deployReceipt.Status)

	cfg := &config.Config{}
	cfg.Web3.ContentRegistryAddress = addr.Hex()
	cfg.Web3.AnvilDeployerKey = anvilKey0
	cfg.Web3.EthereumRPC = rpcURL
	cfg.Web3.ChainID = anvilChainID

	registry := gateway.ProvideContentRegistry(cfg, zap.NewNop())
	require.NotNil(t, registry, "binding must be constructible with the config above")

	binding, ok := registry.(*web3.ContentRegistryBinding)
	require.True(t, ok, "provider must return the concrete binding")

	contentID := time.Now().Format("it-20060102-150405")
	creator := crypto.PubkeyToAddress(key.PublicKey).Hex()
	digest := sha256.Sum256([]byte("original-media-bytes-" + contentID))

	existsBefore, err := binding.ContentExists(ctx, contentID)
	require.NoError(t, err)
	require.False(t, existsBefore, "content must not exist before registration")

	txHash, err := binding.RegisterContent(ctx, contentID, creator, digest, "ipfs://integration-test")
	require.NoError(t, err, "RegisterContent tx must be accepted by the deployed contract")
	require.True(t, len(txHash) >= 66, "tx hash must look like a hash: %s", txHash)

	// Real receipt + event: the log's first topic must be the ContentRegistered
	// signature, which proves the Go ABI and the deployed contract agree.
	var receipt *types.Receipt
	require.Eventually(t, func() bool {
		r, err := ethClient.TransactionReceipt(ctx, common.HexToHash(txHash))
		if err != nil || r == nil {
			return false
		}
		receipt = r
		return true
	}, 20*time.Second, 300*time.Millisecond, "registration receipt must become available")
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	require.NotEmpty(t, receipt.Logs, "registration must emit an event")
	require.Equal(t, binding.ContentRegisteredTopic(), receipt.Logs[0].Topics[0])

	require.Eventually(t, func() bool {
		exists, err := binding.ContentExists(ctx, contentID)
		return err == nil && exists
	}, 15*time.Second, 500*time.Millisecond, "content must exist on-chain after registration")

	record, err := binding.GetContent(ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, contentID, record.ContentID)
	require.Equal(t, creator, record.Creator)
	require.Equal(t, digest, record.ContentDigest)
	require.Equal(t, "ipfs://integration-test", record.MetadataURI)
}
