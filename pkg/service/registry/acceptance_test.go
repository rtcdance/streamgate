package registry_test

// SG-13 A acceptance tests. They run against a real Postgres because the
// semantics under test (advisory-locked nonce allocation, FOR UPDATE SKIP
// LOCKED claims, unique constraints across concurrent writers) only exist
// there; point STREAMGATE_TEST_DB_DSN at a SCRATCH database — the tests
// delete only their own rows (rows scoped to chain id 991337).

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/rtcdance/streamgate/pkg/service/content"
	"github.com/rtcdance/streamgate/pkg/service/registry"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3/tx"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	testChainID  int64 = 991337
	testRegistry       = "0x00000000000000000000000000000000000REG" // lowercased in store
	testSigner         = "0x51337000000000000000000000000000000000f1"
	testWallet         = "0xAbC00000000000000000000000000000000000a1"
)

func openRegistryTestDB(t *testing.T) *storage.Database {
	t.Helper()
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set; SG-13 registry durability tests need a scratch Postgres")
	}
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	if err != nil {
		t.Skipf("registry test DB unavailable: %v", err)
	}
	if err := db.Ping(context.Background()); err != nil {
		t.Skipf("registry test DB unreachable: %v", err)
	}
	if err := helpers.ApplyMigrations(t, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	cleanupRegistryTestRows(t, db)
	t.Cleanup(func() {
		cleanupRegistryTestRows(t, db)
		_ = db.Close()
	})
	return db
}

func cleanupRegistryTestRows(t *testing.T, db *storage.Database) {
	t.Helper()
	ctx := context.Background()
	// Contents rows are reachable through their intents (contents.id is a UUID,
	// so no id-prefix match is possible).
	for _, q := range []string{
		`DELETE FROM contents c USING content_registry_intents i WHERE c.id = i.content_id::uuid AND i.chain_id = $1`,
		`DELETE FROM content_registry_intents WHERE chain_id = $1`,
		`DELETE FROM tx_nonce_reservations WHERE chain_id = $1`,
		`DELETE FROM tx_signed_attempts WHERE chain_id = $1`,
		`DELETE FROM http_idempotency WHERE scope LIKE 'chain:' || $1 || ':%'`,
	} {
		if _, err := db.Exec(ctx, q, testChainID); err != nil {
			t.Fatalf("cleanup %q: %v", q, err)
		}
	}
}

// ---- fakes -----------------------------------------------------------------

// fakeChain is a scripted ChainClient: send errors are queued per call and a
// queued entry can record the tx BEFORE failing, simulating "node accepted,
// HTTP/RPC answer lost".
type fakeChain struct {
	mu            sync.Mutex
	consumedNonce uint64
	receipts      map[common.Hash]*types.Receipt
	headers       map[uint64]*types.Header
	head          uint64
	sendPlan      []func(*types.Transaction) error
	sentHashes    []common.Hash
}

func newFakeChain() *fakeChain {
	return &fakeChain{
		receipts: make(map[common.Hash]*types.Receipt),
		headers:  make(map[uint64]*types.Header),
	}
}

func (f *fakeChain) GetNonce(_ context.Context, _ string) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.consumedNonce, nil
}

func (f *fakeChain) SendTransaction(_ context.Context, t *types.Transaction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentHashes = append(f.sentHashes, t.Hash())
	var step func(*types.Transaction) error
	if len(f.sendPlan) > 0 {
		step = f.sendPlan[0]
		f.sendPlan = f.sendPlan[1:]
	}
	if step != nil {
		return step(t)
	}
	return nil
}

func (f *fakeChain) TransactionReceipt(_ context.Context, hash common.Hash) (*types.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.receipts[hash]; ok {
		return r, nil
	}
	return nil, ethereum.NotFound
}

func (f *fakeChain) GetBlockNumber(_ context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head, nil
}

func (f *fakeChain) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if number == nil {
		if h, ok := f.headers[f.head]; ok {
			return h, nil
		}
		return nil, errors.New("header not found")
	}
	if h, ok := f.headers[number.Uint64()]; ok {
		return h, nil
	}
	return nil, errors.New("header not found")
}

// fakePipeline signs real (but payload-less) transactions so raw bytes round
// trip through MarshalBinary/UnmarshalBinary exactly like production.
type fakePipeline struct {
	client       *fakeChain
	key          *ecdsa.PrivateKey
	chainID      *big.Int
	mu           sync.Mutex
	prepareCalls int
}

func newFakePipeline(t *testing.T, client *fakeChain) *fakePipeline {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	return &fakePipeline{client: client, key: key, chainID: big.NewInt(testChainID)}
}

func (p *fakePipeline) PrepareRegisterContent(_ context.Context, contentID, _ string, _ [32]byte, _ string, nonce uint64) (*tx.SignedTx, error) {
	p.mu.Lock()
	p.prepareCalls++
	p.mu.Unlock()
	to := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	unsigned := types.NewTransaction(nonce, to, big.NewInt(0), 21000, big.NewInt(1), []byte(contentID))
	signed, err := types.SignTx(unsigned, types.LatestSignerForChainID(p.chainID), p.key)
	if err != nil {
		return nil, err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &tx.SignedTx{Raw: raw, Hash: signed.Hash().Hex(), Nonce: nonce}, nil
}

func (p *fakePipeline) Broadcast(ctx context.Context, signed *tx.SignedTx) error {
	var decoded types.Transaction
	if err := decoded.UnmarshalBinary(signed.Raw); err != nil {
		return err
	}
	return p.client.SendTransaction(ctx, &decoded)
}

func (p *fakePipeline) prepares() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.prepareCalls
}

// ---- A: idempotency --------------------------------------------------------

func TestAcceptance_RegistryIdempotency(t *testing.T) {
	db := openRegistryTestDB(t)
	ctx := context.Background()

	store := registry.NewStore(db, testChainID, testRegistry)
	idem := registry.NewIdempotencyStore(db)
	attestSvc := registry.NewService(store, idem)

	contentSvc := content.NewContentService(db, nil, nil, zap.NewNop())
	contentSvc.SetRegistryIntent(store)

	contentID := uuid.NewString()
	digest1 := strings.Repeat("ab", 32)
	digest2 := strings.Repeat("cd", 32)

	// 20 concurrent creations for the SAME business key: exactly one contents
	// row and exactly ONE intent row may exist afterwards.
	var wg sync.WaitGroup
	succeeded := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := contentSvc.CreateContentWithTx(ctx, &content.Content{
				ID:       contentID,
				Title:    "SG13 Idempotency",
				Type:     "video",
				OwnerID:  testWallet,
				FileHash: digest1,
			})
			if err == nil {
				succeeded <- id
			}
		}()
	}
	wg.Wait()
	close(succeeded)
	require.Len(t, succeeded, 1, "exactly one concurrent CreateContentWithTx may win")
	assert.Equal(t, contentID, <-succeeded)
	assertInt(t, db, `SELECT COUNT(*) FROM content_registry_intents WHERE chain_id=$1 AND content_id=$2`, 1, testChainID, contentID)

	// Attest requires the sealed, verified original (digest comes from
	// contents.file_hash + original_verified, never from the client); mark it
	// the way the upload sealing path does.
	_, err := db.Exec(ctx, `UPDATE contents SET original_verified = TRUE WHERE id = $1`, contentID)
	require.NoError(t, err)

	// Real HTTP surface: the registered attest route, wallet injected.
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("wallet_address", testWallet); c.Next() })
	gateway.RegisterContentRoutes(router, zap.NewNop(), contentSvc, attestSvc)

	const idemKey = "client-key-1"
	attest := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/content/"+contentID+"/attest", http.NoBody)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// 20 concurrent attests with the SAME Idempotency-Key: same response, no
	// extra rows.
	var hwg sync.WaitGroup
	bodies := make(chan string, 20)
	codes := make(chan int, 20)
	for i := 0; i < 20; i++ {
		hwg.Add(1)
		go func() {
			defer hwg.Done()
			w := attest(idemKey)
			codes <- w.Code
			bodies <- w.Body.String()
		}()
	}
	hwg.Wait()
	close(codes)
	close(bodies)
	for code := range codes {
		assert.Equal(t, http.StatusOK, code)
	}
	var first string
	for body := range bodies {
		if first == "" {
			first = body
		}
		assert.Equal(t, first, body, "same key + same payload must replay the original response")
	}
	assertInt(t, db, `SELECT COUNT(*) FROM content_registry_intents WHERE chain_id=$1 AND content_id=$2`, 1, testChainID, contentID)
	assertInt(t, db, `SELECT COUNT(*) FROM http_idempotency WHERE scope LIKE 'chain:' || $1 || ':%'`, 1, testChainID)

	// Sequential retry: identical stored response, still no new row.
	retry := attest(idemKey)
	require.Equal(t, http.StatusOK, retry.Code)
	assert.Equal(t, first, retry.Body.String())

	// Absent key: relies on the unique intent alone (still one row, 200).
	noKey := attest("")
	require.Equal(t, http.StatusOK, noKey.Code)
	assertInt(t, db, `SELECT COUNT(*) FROM content_registry_intents WHERE chain_id=$1 AND content_id=$2`, 1, testChainID, contentID)

	// Publisher pass: exactly ONE prepare and ONE broadcast for the single intent.
	client := newFakeChain()
	pipeline := newFakePipeline(t, client)
	pub := registry.NewPublisher(store, client, pipeline, registry.PublisherConfig{
		Signer: testSigner, ChainID: testChainID,
	}, zap.NewNop())
	require.NoError(t, pub.RunOnce(ctx))
	assert.Equal(t, 1, pipeline.prepares(), "one intent must produce exactly one signing round")
	assert.Len(t, client.sentHashes, 1, "one intent must produce exactly one broadcast")

	// Same key + DIFFERENT payload (the verified digest changed server-side) → 409.
	_, err = db.Exec(ctx, `UPDATE contents SET file_hash = $1 WHERE id = $2`, digest2, contentID)
	require.NoError(t, err)
	conflict := attest(idemKey)
	require.Equal(t, http.StatusConflict, conflict.Code)

	// A different key with the new payload is a new exchange, not a conflict —
	// but it can NEVER re-register the same content ID under a different
	// digest: the unique (chain, registry, contentID) intent keeps the digest
	// it was first recorded with.
	other := attest("client-key-2")
	require.Equal(t, http.StatusOK, other.Code)
	assert.Contains(t, other.Body.String(), digest1)
	assert.NotContains(t, other.Body.String(), digest2)
	assertInt(t, db, `SELECT COUNT(*) FROM content_registry_intents WHERE chain_id=$1 AND content_id=$2`, 1, testChainID, contentID)
}

// ---- A.3: nonce across instances -------------------------------------------

func TestAcceptance_NonceAcrossInstances(t *testing.T) {
	dbA := openRegistryTestDB(t)
	// A second, INDEPENDENT connection pool = a second app instance sharing
	// only the database.
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	dbB, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbB.Close() })

	storeA := registry.NewStore(dbA, testChainID, testRegistry)
	storeB := registry.NewStore(dbB, testChainID, testRegistry)
	ctx := context.Background()

	const signers = 20
	var wg sync.WaitGroup
	nonces := make(chan uint64, signers)
	for i := 0; i < signers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := storeA
			if i%2 == 1 {
				store = storeB
			}
			// First-time allocation: chain pending nonce is 0 for everyone.
			n, err := store.ReserveNonce(ctx, testSigner, 0)
			if err != nil {
				t.Errorf("reserve nonce: %v", err)
				return
			}
			nonces <- n
		}(i)
	}
	wg.Wait()
	close(nonces)

	seen := make(map[uint64]bool)
	for n := range nonces {
		assert.False(t, seen[n], "duplicate nonce %d allocated across instances", n)
		seen[n] = true
	}
	assert.Len(t, seen, signers, "every instance allocation must yield a distinct nonce")
	count, err := storeA.CountReservations(ctx, testSigner)
	require.NoError(t, err)
	assert.Equal(t, signers, count)

	// An explicit same-nonce replacement keeps multiple attempts but does NOT
	// create a new nonce reservation.
	nonce0 := registry.ReservationKey(testChainID, testSigner, 0)
	require.NoError(t, storeA.InsertAttempt(ctx, &registry.Attempt{
		ReservationKey: nonce0, ChainID: testChainID, Signer: testSigner, Nonce: 0,
		TxHash: common.BytesToHash([]byte("replacement-0")).Hex(), RawTx: "beef", Status: "replaced",
	}))
	require.NoError(t, storeA.InsertAttempt(ctx, &registry.Attempt{
		ReservationKey: nonce0, ChainID: testChainID, Signer: testSigner, Nonce: 0,
		TxHash: common.BytesToHash([]byte("replacement-0b")).Hex(), RawTx: "cafe", Status: "replaced",
	}))
	count, err = storeB.CountReservations(ctx, testSigner)
	require.NoError(t, err)
	assert.Equal(t, signers, count, "replacement must not create a new nonce reservation")

	next, err := storeB.ReserveNonce(ctx, testSigner, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(signers), next, "allocation continues after the replacement")
}

// ---- A.4: broadcast_unknown recovery ---------------------------------------

func TestAcceptance_BroadcastUnknownRecovery(t *testing.T) {
	db := openRegistryTestDB(t)
	ctx := context.Background()
	store := registry.NewStore(db, testChainID, testRegistry)

	contentID := uuid.NewString()
	require.NoError(t, store.EnsureIntent(ctx, contentID, testWallet, strings.Repeat("ef", 32), "ipfs://unknown"))

	// The node ACCEPTS the tx but the RPC answer is lost (timeout). The tx is
	// therefore already known to the node and must never be re-signed.
	var nodeAccepted bool
	timeout := fmt.Errorf("send: %w", context.DeadlineExceeded)
	client := newFakeChain()
	client.sendPlan = []func(*types.Transaction) error{
		func(st *types.Transaction) error {
			nodeAccepted = true // the node has it…
			return timeout      // …but the caller only sees a timeout
		},
		func(*types.Transaction) error { return timeout }, // recovery re-send times out again
	}
	pipeline := newFakePipeline(t, client)
	pub := registry.NewPublisher(store, client, pipeline, registry.PublisherConfig{
		Signer: testSigner, ChainID: testChainID,
	}, zap.NewNop())

	require.NoError(t, pub.RunOnce(ctx))
	require.True(t, nodeAccepted)

	in, err := store.Get(ctx, contentID)
	require.NoError(t, err)
	require.NotNil(t, in)
	assert.Equal(t, registry.StatusBroadcastUnknown, in.Status, "an RPC timeout is broadcast_unknown, never failed")
	assert.NotEmpty(t, in.TxHash, "the known hash must be persisted before broadcast")
	require.True(t, in.Nonce.Valid)
	assert.Equal(t, int64(0), in.Nonce.Int64)

	count, err := store.CountReservations(ctx, testSigner)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "a timeout must NOT roll back or re-allocate the nonce")
	attempt, err := store.GetAttempt(ctx, in.TxHash)
	require.NoError(t, err)
	require.NotNil(t, attempt, "the signed raw tx must be persisted before broadcast")
	assert.NotEmpty(t, attempt.RawTx)
	assert.Equal(t, int64(0), attempt.Nonce)

	// Second pass: still unknown; recovery re-sends the SAME raw tx, never a
	// new nonce or a new signature.
	require.NoError(t, pub.RunOnce(ctx))
	in2, err := store.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusBroadcastUnknown, in2.Status)
	assert.Equal(t, in.TxHash, in2.TxHash)
	assert.Equal(t, 1, pipeline.prepares(), "recovery must never re-sign with a new nonce")
	count, err = store.CountReservations(ctx, testSigner)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Recovery: the receipt for the KNOWN hash appears.
	header := &types.Header{Number: big.NewInt(5), Extra: []byte("block-5")}
	client.mu.Lock()
	client.receipts[common.HexToHash(in.TxHash)] = &types.Receipt{
		Status: types.ReceiptStatusSuccessful, BlockNumber: big.NewInt(5), BlockHash: header.Hash(),
	}
	client.headers[5] = header
	client.head = 7
	client.mu.Unlock()

	require.NoError(t, pub.RunOnce(ctx))
	in3, err := store.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusConfirmed, in3.Status, "receipt + confirmation policy must land on confirmed")
	assert.Equal(t, in.TxHash, in3.TxHash)
	assert.True(t, in3.BlockNumber.Valid)
	assert.Equal(t, int64(5), in3.BlockNumber.Int64)
	assert.Equal(t, header.Hash().Hex(), in3.BlockHash)
	assert.True(t, in3.ConfirmedAt.Valid)

	// Every broadcast of this intent used ONE hash.
	distinct := make(map[common.Hash]bool)
	for _, h := range client.sentHashes {
		distinct[h] = true
	}
	assert.Len(t, distinct, 1, "the exact same tx hash must be re-sent, never a second one")
}

// ---- A.5: receipt is not finality — reorg re-verification -------------------

func TestAcceptance_TxReceiptReorg(t *testing.T) {
	db := openRegistryTestDB(t)
	ctx := context.Background()
	store := registry.NewStore(db, testChainID, testRegistry)

	contentID := uuid.NewString()
	require.NoError(t, store.EnsureIntent(ctx, contentID, testWallet, strings.Repeat("11", 32), "ipfs://reorg"))

	client := newFakeChain()
	pipeline := newFakePipeline(t, client)
	pubCfg := registry.PublisherConfig{Signer: testSigner, ChainID: testChainID, Confirmations: 2}
	pub := registry.NewPublisher(store, client, pipeline, pubCfg, zap.NewNop())

	// 1. Broadcast accepted → submitted (no receipt yet).
	require.NoError(t, pub.RunOnce(ctx))
	in, err := store.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusSubmitted, in.Status)
	require.NotEmpty(t, in.TxHash)

	// 2. Receipt lands at block 5; head is still too close for the policy.
	header5 := &types.Header{Number: big.NewInt(5), Extra: []byte("reorgable")}
	client.mu.Lock()
	client.receipts[common.HexToHash(in.TxHash)] = &types.Receipt{
		Status: types.ReceiptStatusSuccessful, BlockNumber: big.NewInt(5), BlockHash: header5.Hash(),
	}
	client.headers[5] = header5
	client.head = 5
	client.mu.Unlock()
	require.NoError(t, pub.RunOnce(ctx))
	in, err = store.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusSubmitted, in.Status, "receipt.status=1 alone is not finality")
	assert.Equal(t, int64(5), in.BlockNumber.Int64, "the block number must be recorded at receipt time")
	assert.Equal(t, header5.Hash().Hex(), in.BlockHash)

	// 3. Head advances → confirmed with recorded evidence.
	client.mu.Lock()
	client.head = 7
	client.mu.Unlock()
	require.NoError(t, pub.RunOnce(ctx))
	in, err = store.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusConfirmed, in.Status)
	assert.True(t, in.ConfirmedAt.Valid)
	assert.Equal(t, int64(5), in.BlockNumber.Int64)

	// 4. "Restart": a fresh store on a NEW connection reads the same durable
	// history — no in-memory map is involved.
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	db2, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })
	store2 := registry.NewStore(db2, testChainID, testRegistry)
	afterRestart, err := store2.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusConfirmed, afterRestart.Status, "confirmed history must survive a restart")
	assert.Equal(t, in.BlockHash, afterRestart.BlockHash)

	// 5. Reorg: block 5 is replaced. The confirmed intent must revert and
	// re-verify — receipt.status=1 was never finality.
	header5b := &types.Header{Number: big.NewInt(5), Extra: []byte("reorganized")}
	require.NotEqual(t, header5.Hash(), header5b.Hash())
	client.mu.Lock()
	client.headers[5] = header5b
	client.mu.Unlock()
	pub2 := registry.NewPublisher(store2, client, pipeline, pubCfg, zap.NewNop())
	require.NoError(t, pub2.RunOnce(ctx))
	reverted, err := store2.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusSubmitted, reverted.Status, "reorg must revert confirmed → submitted for re-verification")
	assert.False(t, reverted.ConfirmedAt.Valid)
	assert.Contains(t, reverted.LastError, "reorg")

	// 6. The tx gets re-included at block 6 on the new chain; head advances.
	header6 := &types.Header{Number: big.NewInt(6), Extra: []byte("canonical")}
	client.mu.Lock()
	delete(client.receipts, common.HexToHash(in.TxHash))
	client.receipts[common.HexToHash(in.TxHash)] = &types.Receipt{
		Status: types.ReceiptStatusSuccessful, BlockNumber: big.NewInt(6), BlockHash: header6.Hash(),
	}
	client.headers[6] = header6
	client.head = 9
	client.mu.Unlock()
	require.NoError(t, pub2.RunOnce(ctx))

	// 7. Another "restart" confirms the re-verified history durably.
	db3, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db3.Close() })
	store3 := registry.NewStore(db3, testChainID, testRegistry)
	final, err := store3.Get(ctx, contentID)
	require.NoError(t, err)
	assert.Equal(t, registry.StatusConfirmed, final.Status)
	assert.Equal(t, int64(6), final.BlockNumber.Int64, "re-verification must record the canonical block")
	assert.Equal(t, header6.Hash().Hex(), final.BlockHash)
	assert.Equal(t, in.TxHash, final.TxHash, "re-verification must not re-sign or change the tx")
}

// ---- helpers ---------------------------------------------------------------

func assertInt(t *testing.T, db storage.DB, query string, want int, args ...interface{}) {
	t.Helper()
	var got int
	require.NoError(t, db.QueryRow(context.Background(), query, args...).Scan(&got))
	assert.Equal(t, want, got, "query %s", query)
}
