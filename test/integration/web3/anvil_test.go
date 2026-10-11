//go:build anvil

package web3_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rtcdance/streamgate/pkg/web3"
	"go.uber.org/zap"
)

// anvilChainID is the chain these tests deploy to. It is asserted explicitly
// (never left to Anvil's default) so a wrong-chain deploy fails loudly.
const anvilChainID int64 = 31337

// anvilDefaultKeys are the first three deterministic Anvil accounts. Tests use
// index 0 as the deployer/owner and other indices for negative cases.
var anvilDefaultKeys = []string{
	"0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	"0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
	"0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
}

// repoRoot walks up from the test working directory to the module root so the
// Forge artifacts can be located regardless of where `go test` is invoked.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

type forgeArtifact struct {
	ABI      json.RawMessage `json:"abi"`
	Bytecode struct {
		Object string `json:"object"`
	} `json:"bytecode"`
}

// loadArtifact reads a Forge build artifact. A missing artifact is a hard
// failure, never a skip: the suite must not pass without a real compile.
func loadArtifact(t *testing.T, relPath string) (abi.ABI, []byte) {
	t.Helper()
	p := filepath.Join(repoRoot(t), relPath)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("forge artifact %s missing (%v) — run `forge build` in contracts/ first", relPath, err)
	}
	var art forgeArtifact
	if err := json.Unmarshal(raw, &art); err != nil {
		t.Fatalf("decode forge artifact %s: %v", relPath, err)
	}
	parsed, err := abi.JSON(strings.NewReader(string(art.ABI)))
	if err != nil {
		t.Fatalf("parse ABI %s: %v", relPath, err)
	}
	bin := common.FromHex(strings.TrimSpace(art.Bytecode.Object))
	if len(bin) == 0 {
		t.Fatalf("artifact %s has empty bytecode — run `forge build`", relPath)
	}
	return parsed, bin
}

// startAnvil launches a disposable Anvil on an OS-allocated port with an
// explicit chain id. Missing tooling, a failed start, or a readiness timeout
// are fatal by design — the anvil-tagged suite must never silently skip.
//
// --slots-in-an-epoch 1 is required: without it Anvil pins the `safe` and
// `finalized` tags to genesis, so the safe-tagged reads the production
// verifier issues would never observe a freshly deployed contract. With an
// epoch of one slot the safe head tracks latest-1, letting tests mine forward
// until a deployment is genuinely inside the safe view (see mineUntilSafe).
func startAnvil(t *testing.T) (rpcURL string, cleanup func()) {
	t.Helper()

	anvilPath, err := exec.LookPath("anvil")
	if err != nil {
		t.Fatalf("anvil not found in PATH — install Foundry to run the anvil-tagged suite (no skip)")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	rpcURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, anvilPath,
		"--port", fmt.Sprint(port),
		"--chain-id", fmt.Sprint(anvilChainID),
		"--gas-limit", "30000000",
		"--accounts", "3",
		"--slots-in-an-epoch", "1",
		"--silent",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start anvil: %v", err)
	}
	cleanup = func() {
		_ = cmd.Process.Kill()
		cancel()
		_ = cmd.Wait()
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := ethclient.Dial(rpcURL)
		if dialErr == nil {
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, pingErr := conn.BlockNumber(pingCtx)
			pingCancel()
			conn.Close()
			if pingErr == nil {
				return rpcURL, cleanup
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	cleanup()
	t.Fatalf("anvil not ready within 20s at %s", rpcURL)
	return "", nil
}

// mineUntilSafe mines blocks until the chain's `safe` head reaches target, so a
// test only asserts on state that the production safe-tagged read can see.
func mineUntilSafe(t *testing.T, rpcURL string, target uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rc, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatalf("dial rpc for mining: %v", err)
	}
	defer rc.Close()

	type header struct {
		Number string `json:"number"`
	}
	for i := 0; i < 64; i++ {
		var h header
		if err := rc.CallContext(ctx, &h, "eth_getBlockByNumber", "safe", false); err != nil {
			t.Fatalf("eth_getBlockByNumber(safe): %v", err)
		}
		var safeNum uint64
		if h.Number != "" {
			safeNum, err = strconv.ParseUint(strings.TrimPrefix(h.Number, "0x"), 16, 64)
			if err != nil {
				t.Fatalf("parse safe block number %q: %v", h.Number, err)
			}
		}
		if safeNum >= target {
			return
		}
		var res any
		if err := rc.CallContext(ctx, &res, "anvil_mine", "0x1"); err != nil {
			t.Fatalf("anvil_mine: %v", err)
		}
	}
	t.Fatalf("safe head never reached block %d", target)
}

func anvilKey(t *testing.T, index int) *ecdsa.PrivateKey {
	t.Helper()
	if index < 0 || index >= len(anvilDefaultKeys) {
		t.Fatalf("anvil key index %d out of range", index)
	}
	hexStr := strings.TrimPrefix(anvilDefaultKeys[index], "0x")
	key, err := crypto.HexToECDSA(hexStr)
	if err != nil {
		t.Fatalf("parse anvil key %d: %v", index, err)
	}
	return key
}

func transactor(t *testing.T, key *ecdsa.PrivateKey) *bind.TransactOpts {
	t.Helper()
	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(anvilChainID))
	if err != nil {
		t.Fatalf("new keyed transactor: %v", err)
	}
	return auth
}

// deploy deploys a Forge-built contract and waits for a successful receipt.
func deploy(t *testing.T, client *ethclient.Client, auth *bind.TransactOpts, contractABI abi.ABI, bin []byte) (common.Address, *types.Receipt) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth.Context = ctx

	addr, tx, _, err := bind.DeployContract(auth, contractABI, bin, client)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("wait deploy mined: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("deploy reverted (tx %s)", tx.Hash().Hex())
	}
	return addr, receipt
}

func transact(t *testing.T, client *ethclient.Client, auth *bind.TransactOpts, contract *bind.BoundContract, method string, params ...any) *types.Receipt {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth.Context = ctx

	tx, err := contract.Transact(auth, method, params...)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		t.Fatalf("wait %s mined: %v", method, err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("%s reverted (tx %s)", method, tx.Hash().Hex())
	}
	return receipt
}

func callBig(t *testing.T, contract *bind.BoundContract, method string, args ...any) *big.Int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out []any
	if err := contract.Call(&bind.CallOpts{Context: ctx}, &out, method, args...); err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
	if len(out) != 1 {
		t.Fatalf("call %s: expected 1 result, got %d", method, len(out))
	}
	v, ok := out[0].(*big.Int)
	if !ok {
		t.Fatalf("call %s: unexpected result type %T", method, out[0])
	}
	return v
}

func TestAnvil_BlockNumber(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	client, err := web3.NewChainClient(rpcURL, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("connect to anvil: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	blockNum, err := client.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("get block number: %v", err)
	}
	t.Logf("anvil initial block number: %d", blockNum)
}

// TestAnvil_ERC721RealContract deploys the real Forge-compiled DemoNFT and
// exercises mint(address) → ownership/balance/info against it.
func TestAnvil_ERC721RealContract(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	erc721ABI, erc721Bin := loadArtifact(t, "contracts/out/DemoNFT.sol/DemoNFT.json")

	ethCli, err := ethclient.Dial(rpcURL)
	if err != nil {
		t.Fatalf("dial anvil: %v", err)
	}
	defer ethCli.Close()

	auth := transactor(t, anvilKey(t, 0))
	walletAddr := auth.From

	contractAddr, _ := deploy(t, ethCli, auth, erc721ABI, erc721Bin)
	contract := bind.NewBoundContract(contractAddr, erc721ABI, ethCli, ethCli, ethCli)

	mintReceipt := transact(t, ethCli, auth, contract, "mint", walletAddr)
	mineUntilSafe(t, rpcURL, mintReceipt.BlockNumber.Uint64())

	if got := callBig(t, contract, "totalSupply"); got.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("totalSupply after one mint = %s, want 1", got.String())
	}
	if got := callBig(t, contract, "balanceOf", walletAddr); got.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("balanceOf(owner) = %s, want 1", got.String())
	}

	chainClient, err := web3.NewChainClient(rpcURL, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("create chain client: %v", err)
	}
	defer chainClient.Close()

	verifier := web3.NewNFTVerifier(chainClient, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	verified, err := verifier.VerifyNFTOwnership(ctx, contractAddr.Hex(), "1", walletAddr.Hex())
	if err != nil {
		t.Fatalf("VerifyNFTOwnership: %v", err)
	}
	if !verified {
		t.Fatal("expected wallet to own token 1")
	}

	balance, err := verifier.GetNFTBalance(ctx, contractAddr.Hex(), walletAddr.Hex())
	if err != nil {
		t.Fatalf("GetNFTBalance: %v", err)
	}
	if balance.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("NFT balance = %s, want 1", balance.String())
	}

	info, err := verifier.GetNFTInfo(ctx, contractAddr.Hex(), "1")
	if err != nil {
		t.Fatalf("GetNFTInfo: %v", err)
	}
	if info.Name != "StreamGate Demo" {
		t.Fatalf("contract name = %q, want %q", info.Name, "StreamGate Demo")
	}
}

func TestAnvil_NFTVerification_NoOwnership(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	erc721ABI, erc721Bin := loadArtifact(t, "contracts/out/DemoNFT.sol/DemoNFT.json")

	ethCli, err := ethclient.Dial(rpcURL)
	if err != nil {
		t.Fatalf("dial anvil: %v", err)
	}
	defer ethCli.Close()

	auth := transactor(t, anvilKey(t, 0))
	walletAddr := auth.From
	otherAddr := crypto.PubkeyToAddress(anvilKey(t, 1).PublicKey)

	contractAddr, _ := deploy(t, ethCli, auth, erc721ABI, erc721Bin)
	contract := bind.NewBoundContract(contractAddr, erc721ABI, ethCli, ethCli, ethCli)
	mintReceipt := transact(t, ethCli, auth, contract, "mint", walletAddr)
	mineUntilSafe(t, rpcURL, mintReceipt.BlockNumber.Uint64())

	chainClient, err := web3.NewChainClient(rpcURL, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("create chain client: %v", err)
	}
	defer chainClient.Close()

	verifier := web3.NewNFTVerifier(chainClient, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	verified, err := verifier.VerifyNFTOwnership(ctx, contractAddr.Hex(), "1", otherAddr.Hex())
	if err != nil {
		t.Fatalf("VerifyNFTOwnership: %v", err)
	}
	if verified {
		t.Fatal("expected non-owner to be rejected")
	}
}

// TestAnvil_ERC1155Quantity deploys the ERC-1155 fixture and proves the same
// wallet can hold 1 then 2 units of a specific token id, so downstream
// quantity gating (min=2) has real on-chain evidence.
func TestAnvil_ERC1155Quantity(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	erc1155ABI, erc1155Bin := loadArtifact(t, "contracts/out/DemoERC1155.sol/DemoERC1155.json")

	ethCli, err := ethclient.Dial(rpcURL)
	if err != nil {
		t.Fatalf("dial anvil: %v", err)
	}
	defer ethCli.Close()

	auth := transactor(t, anvilKey(t, 0))
	walletAddr := auth.From

	contractAddr, _ := deploy(t, ethCli, auth, erc1155ABI, erc1155Bin)
	contract := bind.NewBoundContract(contractAddr, erc1155ABI, ethCli, ethCli, ethCli)

	const tokenID = 7

	mint1 := transact(t, ethCli, auth, contract, "mint", walletAddr, big.NewInt(tokenID), big.NewInt(1))
	mineUntilSafe(t, rpcURL, mint1.BlockNumber.Uint64())
	if got := callBig(t, contract, "balanceOf", walletAddr, big.NewInt(tokenID)); got.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("erc1155 balanceOf after 1 unit = %s, want 1", got.String())
	}

	chainClient, err := web3.NewChainClient(rpcURL, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("create chain client: %v", err)
	}
	defer chainClient.Close()

	verifier := web3.NewNFTVerifier(chainClient, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	qty1, err := verifier.GetNFTBalanceAutoDetect(ctx, contractAddr.Hex(), walletAddr.Hex(), fmt.Sprint(tokenID))
	if err != nil {
		t.Fatalf("GetNFTBalanceAutoDetect (1 unit): %v", err)
	}
	if qty1.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("auto-detected ERC-1155 quantity = %s, want 1", qty1.String())
	}

	mint2 := transact(t, ethCli, auth, contract, "mint", walletAddr, big.NewInt(tokenID), big.NewInt(1))
	mineUntilSafe(t, rpcURL, mint2.BlockNumber.Uint64())

	qty2, err := verifier.GetNFTBalanceAutoDetect(ctx, contractAddr.Hex(), walletAddr.Hex(), fmt.Sprint(tokenID))
	if err != nil {
		t.Fatalf("GetNFTBalanceAutoDetect (2 units): %v", err)
	}
	if qty2.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("auto-detected ERC-1155 quantity = %s, want 2", qty2.String())
	}
}

func TestAnvil_RPCFailover(t *testing.T) {
	goodURL, cleanupAnvil := startAnvil(t)
	defer cleanupAnvil()

	badURL := "http://127.0.0.1:19999"
	client, err := web3.NewChainClientWithFallback([]string{badURL, goodURL}, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("create chain client with fallback: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	blockNum, err := client.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("GetBlockNumber after failover: %v", err)
	}
	t.Logf("failed over from bad RPC, block number: %d", blockNum)

	erc721ABI, erc721Bin := loadArtifact(t, "contracts/out/DemoNFT.sol/DemoNFT.json")

	ethCli, err := ethclient.Dial(goodURL)
	if err != nil {
		t.Fatalf("dial anvil: %v", err)
	}
	defer ethCli.Close()

	auth := transactor(t, anvilKey(t, 0))
	walletAddr := auth.From

	contractAddr, _ := deploy(t, ethCli, auth, erc721ABI, erc721Bin)
	contract := bind.NewBoundContract(contractAddr, erc721ABI, ethCli, ethCli, ethCli)
	mintReceipt := transact(t, ethCli, auth, contract, "mint", walletAddr)
	mineUntilSafe(t, goodURL, mintReceipt.BlockNumber.Uint64())

	verifier := web3.NewNFTVerifier(client, zap.NewNop())
	verified, err := verifier.VerifyNFTOwnership(ctx, contractAddr.Hex(), "1", walletAddr.Hex())
	if err != nil {
		t.Fatalf("VerifyNFTOwnership via failover: %v", err)
	}
	if !verified {
		t.Fatal("expected ownership verification to succeed via failover RPC")
	}
}

func TestAnvil_ChainBlockTags(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	chainClient, err := web3.NewChainClient(rpcURL, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("create chain client: %v", err)
	}
	defer chainClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	header, err := chainClient.HeaderByNumber(ctx, nil)
	if err != nil {
		t.Fatalf("HeaderByNumber(latest): %v", err)
	}
	t.Logf("latest block: %d hash=%s", header.Number.Uint64(), header.Hash().Hex())
}

func TestAnvil_HealthCheck(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	client, err := web3.NewChainClient(rpcURL, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("create chain client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("health check: %v", err)
	}
}

func TestAnvil_RateLimiter(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	client, err := web3.NewChainClient(rpcURL, anvilChainID, zap.NewNop())
	if err != nil {
		t.Fatalf("create chain client: %v", err)
	}
	defer client.Close()

	rl := web3.NewRPCRateLimiter(1000, 2000, zap.NewNop())
	client.SetRateLimiter(rl)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	blockNum, err := client.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("GetBlockNumber with rate limiter: %v", err)
	}
	t.Logf("block number with rate limiter: %d", blockNum)
}
