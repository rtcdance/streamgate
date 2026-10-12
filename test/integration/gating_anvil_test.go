//go:build anvil

package integration_test

// SG-04 end-to-end: two real Anvils behind recording RPC proxies.
//
// Everything here is a real node and real JSON-RPC — no mocked balances. It
// proves the three claims the unit tests can only assert against a mock:
//
//  1. two chains at the SAME height carry different hashes, so block evidence
//     from one chain can never stand in for another;
//  2. a pinned read actually sends `eth_call` with the pinned block number, and
//     the ERC-165 probe and the balance query name the SAME block;
//  3. a client that cannot pin is refused instead of silently reading latest.
//
// Run with: go test -tags anvil ./test/integration/ -run TestAnvil_BlockEvidence

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

// deployContract deploys a real contract through the recording proxy, so the
// deployment traffic is visible to the recorder too.
func deployContract(t *testing.T, ctx context.Context, rpcURL string, chainID int64, key *ecdsa.PrivateKey, contractABI abi.ABI, bin []byte) common.Address {
	t.Helper()
	client, err := ethclient.Dial(rpcURL)
	require.NoError(t, err)
	defer client.Close()

	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(chainID))
	require.NoError(t, err)
	auth.Context = ctx

	addr, tx, _, err := bind.DeployContract(auth, contractABI, bin, client)
	require.NoError(t, err)
	receipt, err := bind.WaitMined(ctx, client, tx)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "deployment must succeed")
	return addr
}

// ---------------------------------------------------------------------------
// Recording RPC proxy
// ---------------------------------------------------------------------------

type recordedCall struct {
	method string
	// block is the eth_call block parameter exactly as it went over the wire.
	// "latest" and "" mean the caller did not pin a block.
	block string
}

type rpcRecorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (r *rpcRecorder) record(method, block string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{method: method, block: block})
}

// ethCallBlocks returns the block parameter of every eth_call seen so far.
func (r *rpcRecorder) ethCallBlocks() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var blocks []string
	for _, c := range r.calls {
		if c.method == "eth_call" {
			blocks = append(blocks, c.block)
		}
	}
	return blocks
}

func (r *rpcRecorder) methods() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := map[string]int{}
	for _, c := range r.calls {
		counts[c.method]++
	}
	return counts
}

// newRecordingProxy forwards JSON-RPC to upstream and records every call.
func newRecordingProxy(t *testing.T, upstream string) (string, *rpcRecorder) {
	t.Helper()
	rec := &rpcRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rec.inspect(body)

		resp, err := http.Post(upstream, "application/json", bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, rec
}

// inspect records the method/block of a single request or a batch.
func (r *rpcRecorder) inspect(body []byte) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return
	}
	if trimmed[0] == '[' {
		var batch []json.RawMessage
		if json.Unmarshal(trimmed, &batch) == nil {
			for _, item := range batch {
				r.inspectOne(item)
			}
		}
		return
	}
	r.inspectOne(trimmed)
}

func (r *rpcRecorder) inspectOne(raw []byte) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &req) != nil || req.Method == "" {
		return
	}
	block := ""
	if req.Method == "eth_call" && len(req.Params) > 1 {
		block = strings.Trim(string(req.Params[1]), `"`)
	}
	r.record(req.Method, block)
}

// ---------------------------------------------------------------------------
// Anvil helpers
// ---------------------------------------------------------------------------

// startAnvilOnChain starts a real anvil with an explicit chain id. It fails
// rather than skipping: an unrun test is not a passing test.
func startAnvilOnChain(t *testing.T, chainID int64) string {
	t.Helper()
	anvilPath, err := exec.LookPath("anvil")
	require.NoError(t, err, "anvil not found in PATH — no skip by design")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	rpcURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, anvilPath,
		"--port", fmt.Sprint(port),
		"--chain-id", fmt.Sprint(chainID),
		"--accounts", "3",
		"--silent",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		cancel()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		client, dialErr := ethclient.Dial(rpcURL)
		if dialErr == nil {
			pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, perr := client.BlockNumber(pctx)
			pcancel()
			client.Close()
			if perr == nil {
				return rpcURL
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("anvil (chain %d) not ready within 20s at %s", chainID, rpcURL)
	return ""
}

// rpcCall issues a raw JSON-RPC request and returns the decoded result.
func rpcCall(t *testing.T, rpcURL, method string, params ...interface{}) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	require.NoError(t, err)
	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.Nil(t, envelope.Error, "%s failed: %v", method, envelope.Error)
	return envelope.Result
}

func blockNumber(t *testing.T, rpcURL string) uint64 {
	t.Helper()
	var hexHeight string
	require.NoError(t, json.Unmarshal(rpcCall(t, rpcURL, "eth_blockNumber"), &hexHeight))
	n, err := strconv.ParseUint(strings.TrimPrefix(hexHeight, "0x"), 16, 64)
	require.NoError(t, err, "unexpected eth_blockNumber %q", hexHeight)
	return n
}

// mineTo mines blocks on rpcURL until its height reaches target.
func mineTo(t *testing.T, rpcURL string, target uint64) {
	t.Helper()
	for blockNumber(t, rpcURL) < target {
		rpcCall(t, rpcURL, "anvil_mine", "0x1")
	}
}

func loadDemoNFTArtifact(t *testing.T) (abi.ABI, []byte) {
	t.Helper()
	p := filepath.Join(repoRoot(t), "contracts/out/DemoNFT.sol/DemoNFT.json")
	raw, err := readFile(p)
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

// ---------------------------------------------------------------------------
// The test
// ---------------------------------------------------------------------------

// newFailingProxy returns a URL that always answers 502, standing in for a dead
// RPC endpoint.
func newFailingProxy(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// SG-04 step 1: a failing endpoint is replaced by another endpoint on the SAME
// chain, and an endpoint serving the WRONG chain is refused rather than used.
//
// Two independent proxies point at one anvil, so this proves ENDPOINT failover.
// It deliberately does NOT prove node-level tolerance: both proxies reach the
// same node.
func TestAnvil_EndpointFailoverStaysOnTheSameChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	anvil := startAnvilOnChain(t, 31337)
	goodProxy, recGood := newRecordingProxy(t, anvil)
	deadProxy := newFailingProxy(t)

	// (a) The first endpoint is dead; the read must be served by the second.
	client, err := web3.NewChainClientWithFallback([]string{deadProxy, goodProxy}, 31337, zap.NewNop())
	require.NoError(t, err, "a dead primary must not stop the client from connecting")
	defer client.Close()

	header, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, header, "the healthy same-chain endpoint must serve the read")
	assert.NotEmpty(t, recGood.methods(), "the healthy endpoint must have been used")

	// (b) A fallback pointing at a DIFFERENT chain must be refused. Here the
	// only two candidates are a dead endpoint and a wrong-chain endpoint, so
	// constructing the client must fail outright rather than read the wrong chain.
	otherChain := startAnvilOnChain(t, 31338)
	otherProxy, recOther := newRecordingProxy(t, otherChain)

	_, err = web3.NewChainClientWithFallback([]string{deadProxy, otherProxy}, 31337, zap.NewNop())
	require.Error(t, err, "a wrong-chain fallback must be rejected, not used")
	assert.Contains(t, err.Error(), "chain id")

	// The only traffic a wrong-chain endpoint may ever see is the chain-id probe
	// that identifies it as wrong. No data read may be served from it.
	methods := recOther.methods()
	delete(methods, "eth_chainId")
	assert.Empty(t, methods, "no data read may be served from the wrong chain")
}

// SG-04 step 5: an explicit Anvil snapshot/revert produces a REAL reorg - the
// same height ends up with a different hash - and the production ReorgDetector
// reports it. No sleep, no waiting on uncertain finality.
func TestAnvil_SnapshotRevertProducesARealReorg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	anvil := startAnvilOnChain(t, 31337)
	ethClient, err := ethclient.Dial(anvil)
	require.NoError(t, err)
	defer ethClient.Close()

	mineTo(t, anvil, 4)
	height := blockNumber(t, anvil)
	require.Greater(t, height, uint64(1))

	// Snapshot at the current tip, build two more blocks on it, then rewind and
	// rebuild. The blocks ABOVE the snapshot get rewritten, so height+1 comes
	// back with a different hash.
	var snapshot string
	require.NoError(t, json.Unmarshal(rpcCall(t, anvil, "evm_snapshot"), &snapshot))
	mineTo(t, anvil, height+2)

	before, err := ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(height+1))
	require.NoError(t, err)
	require.Equal(t, height+1, before.Number.Uint64())

	reverted := false
	require.NoError(t, json.Unmarshal(rpcCall(t, anvil, "evm_revert", snapshot), &reverted))
	require.True(t, reverted, "evm_revert must succeed")

	// A different timestamp makes the rebuilt blocks differ from the originals.
	rpcCall(t, anvil, "anvil_setNextBlockTimestamp", fmt.Sprintf("0x%x", time.Now().Unix()+3600))
	mineTo(t, anvil, height+2)

	after, err := ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(height+1))
	require.NoError(t, err)
	require.Equal(t, before.Number.Uint64(), after.Number.Uint64(), "same height")
	require.NotEqual(t, before.Hash().Hex(), after.Hash().Hex(),
		"the snapshot/revert must have produced a different block at the same height")

	// The production detector must agree that this is a reorg.
	detector := web3.NewReorgDetector(ethClient, zap.NewNop())
	reorged, err := detector.CheckReorg(ctx, height+1, before.Hash())
	require.NoError(t, err)
	assert.True(t, reorged, "the production ReorgDetector must report the rewrite as a reorg")

	// And an unchanged block is not reported as one.
	reorged, err = detector.CheckReorg(ctx, height+1, after.Hash())
	require.NoError(t, err)
	assert.False(t, reorged, "the current canonical block must not read as a reorg")
}

func TestAnvil_BlockEvidenceIsPinnedAndChainScoped(t *testing.T) {
	anvilA := startAnvilOnChain(t, 31337)
	anvilB := startAnvilOnChain(t, 31338)
	require.NotEqual(t, anvilA, anvilB)

	proxyA, recA := newRecordingProxy(t, anvilA)
	proxyB, recB := newRecordingProxy(t, anvilB)

	nftABI, nftBin := loadDemoNFTArtifact(t)

	// Deploy the real ERC-721 on chain B only.
	key, err := crypto.HexToECDSA(strings.TrimPrefix(anvilKey0, "0x"))
	require.NoError(t, err)
	owner := crypto.PubkeyToAddress(key.PublicKey)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	deployedB := deployContract(t, ctx, proxyB, 31338, key, nftABI, nftBin)
	heightB := blockNumber(t, anvilB)
	require.Greater(t, heightB, uint64(0))

	// Bring chain A to the SAME height so the two chains are directly
	// comparable at one height.
	mineTo(t, anvilA, heightB)
	heightA := blockNumber(t, anvilA)
	require.Equal(t, heightB, heightA, "both chains must sit at the same height for this comparison")

	clientA, err := web3.NewChainClient(proxyA, 31337, zap.NewNop())
	require.NoError(t, err)
	defer clientA.Close()
	clientB, err := web3.NewChainClient(proxyB, 31338, zap.NewNop())
	require.NoError(t, err)
	defer clientB.Close()

	// (1) Same height, different hash. This is the premise the whole chain-scoped
	// evidence design rests on, checked against real nodes.
	headerA, err := clientA.HeaderByNumber(ctx, new(big.Int).SetUint64(heightA))
	require.NoError(t, err)
	headerB, err := clientB.HeaderByNumber(ctx, new(big.Int).SetUint64(heightB))
	require.NoError(t, err)
	require.NotNil(t, headerA)
	require.NotNil(t, headerB)
	assert.Equal(t, heightA, headerA.Number.Uint64())
	assert.Equal(t, heightB, headerB.Number.Uint64())
	assert.NotEqual(t, headerA.Hash().Hex(), headerB.Hash().Hex(),
		"two chains at the same height must not share a block hash")

	// (2) A pinned read names the pinned block on the wire — for the ERC-165
	// probe AND the balance query. Only the calls made from here on count; the
	// deployment already put its own eth_calls through this proxy.
	pinned := heightB
	beforePinned := len(recB.ethCallBlocks())
	verifier := web3.NewNFTVerifier(clientB, zap.NewNop()).WithBlockNumber(new(big.Int).SetUint64(pinned))

	quantity, err := verifier.GetNFTQuantityAutoDetect(ctx, deployedB.Hex(), "", owner.Hex())
	require.NoError(t, err, "the pinned read must succeed against a real node")
	require.NotNil(t, quantity)

	blocks := recB.ethCallBlocks()[beforePinned:]
	require.NotEmpty(t, blocks, "the pinned read must have issued eth_call traffic")
	wantBlock := fmt.Sprintf("0x%x", pinned)
	for _, got := range blocks {
		assert.Equal(t, wantBlock, got,
			"every eth_call must name the pinned block; got %q", got)
	}

	// The chain-B authorization must not have touched chain A at all: evidence
	// read from one chain can never be used to justify a rule on another.
	assert.Empty(t, recA.ethCallBlocks(),
		"an authorization on chain B must not issue eth_call against chain A")

	// (3) A client that cannot pin is refused, not silently read at latest.
	plainClient, err := ethclient.Dial(proxyB)
	require.NoError(t, err)
	defer plainClient.Close()

	before := len(recB.ethCallBlocks())
	unpinnable := web3.NewNFTVerifier(plainClient, zap.NewNop()).WithBlockNumber(new(big.Int).SetUint64(pinned))
	_, err = unpinnable.GetNFTQuantityAutoDetect(ctx, deployedB.Hex(), "", owner.Hex())
	require.Error(t, err, "a client that cannot pin must be refused")
	assert.Equal(t, before, len(recB.ethCallBlocks()),
		"no chain read may happen when the client cannot pin")
}
