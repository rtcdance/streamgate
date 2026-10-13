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
	"encoding/hex"
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
	"github.com/google/uuid"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/rtcdance/streamgate/pkg/service/content"
	"github.com/rtcdance/streamgate/pkg/service/registry"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/rtcdance/streamgate/test/helpers"
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

// TestAnvil_RegistryDurableIntentLifecycle drives the SG-13 durable path end
// to end: content creation records the register intent in the SAME DB
// transaction as the contents row, the registry publisher signs (raw tx
// persisted BEFORE broadcast), broadcasts, waits for a real receipt plus the
// confirmation policy, and the confirmed history — block evidence, raw tx,
// creator/digest association — is readable from Postgres after a simulated
// restart. Requires STREAMGATE_TEST_DB_DSN (scratch database).
func TestAnvil_RegistryDurableIntentLifecycle(t *testing.T) {
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set; the durable lifecycle test needs a scratch Postgres")
	}
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.Ping(context.Background()))
	require.NoError(t, helpers.ApplyMigrations(t, db))

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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	auth.Context = ctx
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

	// Each run starts a FRESH Anvil chain, so the persisted nonce ledger for
	// this (chain, signer) belongs to a chain that no longer exists and must
	// be reset — exactly what an operator does after a chain reset. Scoping to
	// (31337, deployer) touches nothing else in the scratch database.
	deployer := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	for _, q := range []struct {
		query string
		args  []interface{}
	}{
		{`DELETE FROM tx_signed_attempts WHERE chain_id = $1 AND signer = $2`, []interface{}{anvilChainID, deployer}},
		{`DELETE FROM tx_nonce_reservations WHERE chain_id = $1 AND signer = $2`, []interface{}{anvilChainID, deployer}},
		{`DELETE FROM content_registry_intents WHERE chain_id = $1 AND registry_address = $2`, []interface{}{anvilChainID, strings.ToLower(addr.Hex())}},
		{`DELETE FROM http_idempotency WHERE scope = $1`, []interface{}{registry.AttestScope(anvilChainID, addr.Hex())}},
	} {
		_, err := db.Exec(ctx, q.query, q.args...)
		require.NoError(t, err)
	}

	hub := gateway.ProvideRegistryHub(cfg, zap.NewNop(), db)
	require.NotNil(t, hub, "hub must be constructible against real Anvil + real Postgres")

	// Content creation writes the intent in the same transaction as the row.
	contentSvc := content.NewContentService(db, nil, nil, zap.NewNop())
	contentSvc.SetRegistryIntent(hub.Store)
	contentID := uuid.NewString()
	creator := crypto.PubkeyToAddress(key.PublicKey).Hex()
	digest := sha256.Sum256([]byte("durable-original-bytes-" + contentID))
	digestHex := hex.EncodeToString(digest[:])
	_, err = contentSvc.CreateContentWithTx(ctx, &content.Content{
		ID: contentID, Title: "Durable", Type: "video", OwnerID: creator, FileHash: digestHex,
	})
	require.NoError(t, err)
	in, err := hub.Store.Get(ctx, contentID)
	require.NoError(t, err)
	require.NotNil(t, in)
	require.Equal(t, registry.StatusPending, in.Status, "the intent must exist as pending before any chain interaction")

	// Attest requires the sealed, verified original the way upload sealing
	// records it; the digest itself still comes only from contents.file_hash.
	_, err = db.Exec(ctx, `UPDATE contents SET original_verified = TRUE WHERE id = $1`, contentID)
	require.NoError(t, err)

	// Drive the real publisher until the confirmation policy is satisfied;
	// evm_mine supplies the confirmation blocks Anvil will not produce by itself.
	mineTwice := func() {
		for i := 0; i < 2; i++ {
			_ = ethClient.Client().CallContext(ctx, nil, "evm_mine")
		}
	}
	var confirmed *registry.Intent
	require.Eventually(t, func() bool {
		if err := hub.Publisher.RunOnce(ctx); err != nil {
			return false
		}
		mineTwice()
		cur, gerr := hub.Store.Get(ctx, contentID)
		if gerr != nil || cur == nil {
			return false
		}
		if cur.Status == registry.StatusConfirmed {
			confirmed = cur
			return true
		}
		return false
	}, 45*time.Second, 500*time.Millisecond, "intent must reach confirmed on real Anvil")

	require.NotEmpty(t, confirmed.TxHash)
	require.True(t, confirmed.BlockNumber.Valid, "the receipt block number must be recorded")
	require.NotEmpty(t, confirmed.BlockHash, "the receipt block hash must be recorded")
	require.True(t, confirmed.ConfirmedAt.Valid)

	// The signed raw tx was persisted (and is replayable), and its receipt is real.
	attempt, err := hub.Store.GetAttempt(ctx, confirmed.TxHash)
	require.NoError(t, err)
	require.NotNil(t, attempt, "the signed raw tx must be persisted")
	require.NotEmpty(t, attempt.RawTx)
	receipt, err := ethClient.TransactionReceipt(ctx, common.HexToHash(confirmed.TxHash))
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	require.Equal(t, confirmed.BlockNumber.Int64, receipt.BlockNumber.Int64())

	// Digest/creator association on the REAL contract matches what was
	// persisted — the attestor never masquerades as the uploader.
	record, err := hub.Binding.GetContent(ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, creator, record.Creator)
	require.Equal(t, digest, record.ContentDigest)

	// Attest returns the durable status; the same Idempotency-Key replays it.
	attestResp, conflict, err := hub.Service.Attest(ctx, creator, contentID, "anvil-attest-1")
	require.NoError(t, err)
	require.False(t, conflict)
	require.Equal(t, registry.StatusConfirmed, attestResp.Status)
	replay, conflict, err := hub.Service.Attest(ctx, creator, contentID, "anvil-attest-1")
	require.NoError(t, err)
	require.False(t, conflict)
	require.Equal(t, attestResp, replay, "replayed attest must return the original response")

	// Simulated restart: a NEW store on a NEW connection reads the same
	// durable confirmed history — nothing lived only in process memory.
	db2, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()
	store2 := registry.NewStore(db2, anvilChainID, addr.Hex())
	afterRestart, err := store2.Get(ctx, contentID)
	require.NoError(t, err)
	require.NotNil(t, afterRestart)
	require.Equal(t, registry.StatusConfirmed, afterRestart.Status)
	require.Equal(t, confirmed.TxHash, afterRestart.TxHash)
	require.Equal(t, confirmed.BlockHash, afterRestart.BlockHash)
}
