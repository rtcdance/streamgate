//go:build anvil && integration

package web3_test

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-redis/redis/v8"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	authplugin "github.com/rtcdance/streamgate/pkg/plugins/auth"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type walletRPCProxy struct {
	down     atomic.Bool
	requests atomic.Int64
	server   *httptest.Server
	tape     walletRPCTape
}

type walletChallengeRaceVerifier struct {
	*service.MultiChainSignatureVerifier
	before func(context.Context) error
}

func (v *walletChallengeRaceVerifier) VerifyWalletProof(ctx context.Context, chain int64, scheme, account, message, encoded string) (*signature.AccountProof, error) {
	if v.before != nil {
		if err := v.before(ctx); err != nil {
			return nil, err
		}
	}
	return v.MultiChainSignatureVerifier.VerifyWalletProof(ctx, chain, scheme, account, message, encoded)
}

func concurrentWalletAdmission(t *testing.T, ctx context.Context, verifier *walletChallengeRaceVerifier, services []*service.AuthService, challenge *storage.WalletChallenge, sig string) string {
	t.Helper()
	// Both independent services must have loaded the unused Redis challenge
	// before either can consume it. The actual Anvil proof is still verified;
	// only the scheduling of this TOCTOU regression is controlled.
	entered, release := make(chan struct{}, 2), make(chan struct{})
	releaseBoth := sync.OnceFunc(func() { close(release) })
	defer releaseBoth()
	verifier.before = func(callCtx context.Context) error {
		select {
		case entered <- struct{}{}:
		case <-callCtx.Done():
			return callCtx.Err()
		}
		select {
		case <-release:
			return nil
		case <-callCtx.Done():
			return callCtx.Err()
		}
	}
	type admissionResult struct {
		token string
		err   error
	}
	results := make(chan admissionResult, 2)
	for _, auth := range services {
		go func() {
			issued, loginErr := auth.AuthenticateWithWallet(ctx, challenge.WalletAddress, challenge.ID, sig, challenge.ChainID)
			results <- admissionResult{issued, loginErr}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case failed := <-results:
			t.Fatalf("an instance failed before reaching the admission barrier: %v", failed.err)
		case <-ctx.Done():
			t.Fatal("both instances did not reach real proof verification")
		}
	}
	releaseBoth()
	var token string
	winners := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			winners++
			token = result.token
		} else {
			require.ErrorIs(t, result.err, storage.ErrChallengeUsed)
			require.Empty(t, result.token)
		}
	}
	verifier.before = nil
	require.Equal(t, 1, winners, "one real Redis challenge may admit exactly one concurrent instance")
	return token
}

func newWalletRPCProxy(t *testing.T, target string) *walletRPCProxy {
	t.Helper()
	parsed, err := url.Parse(target)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(parsed)
	result := &walletRPCProxy{}
	result.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result.requests.Add(1)
		if result.down.Load() {
			http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
			return
		}
		result.recordRPC(proxy, w, r)
	}))
	t.Cleanup(result.server.Close)
	return result
}

// Required by the dedicated wallet acceptance target. Missing Redis is fatal,
// never SKIP. This covers real signature RPC and real Redis Lua, not a public
// network, Postgres recovery, browser, or dual-mode media acceptance.
func TestCWLoginLifecycle_RealRedisAnvil(t *testing.T) {
	redisAddress := os.Getenv("W3_REDIS_ADDR")
	require.NotEmpty(t, redisAddress, "dedicated wallet Redis endpoint is required")
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	rpcURL, stop := startAnvil(t)
	defer stop()
	client, err := ethclient.Dial(rpcURL)
	require.NoError(t, err)
	defer client.Close()
	ownerKey := anvilKey(t, 0)
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	nextKey := anvilKey(t, 1)
	nextOwner := crypto.PubkeyToAddress(nextKey.PublicKey)
	walletABI, walletBin := loadArtifact(t, "contracts/out/Study1271Wallet.sol/Study1271Wallet.json")
	wallet, walletContract := deployWalletFixture(t, client, rpcURL, walletABI, walletBin, owner)
	nftABI, nftBin := loadArtifact(t, "contracts/out/DemoNFT.sol/DemoNFT.json")
	nftAddress, _ := deploy(t, client, transactor(t, ownerKey), nftABI, nftBin)
	nft := bind.NewBoundContract(nftAddress, nftABI, client, client, client)
	receipt := transact(t, client, transactor(t, ownerKey), nft, "mint", wallet)
	mineUntilSafe(t, rpcURL, receipt.BlockNumber.Uint64())
	primary := newWalletRPCProxy(t, rpcURL)
	fallback := newWalletRPCProxy(t, rpcURL)
	chain, err := web3.NewChainClientWithFallback([]string{primary.server.URL, fallback.server.URL}, anvilChainID, zap.NewNop())
	require.NoError(t, err)
	manager := web3.NewMultiChainManager(zap.NewNop())
	manager.AddChainWithClient(anvilChainID, chain)
	defer manager.Close()
	// The shared recovery authority is a local model in this focused test.
	// Its Postgres-backed wiring is covered by the full-stack recovery group.
	restore := storage.NewMemoryRestoreEpochStore()
	verifier := &walletChallengeRaceVerifier{MultiChainSignatureVerifier: service.NewMultiChainSignatureVerifier(zap.NewNop(), nil, manager)}
	var ownedKeys []string
	newService := func() (*service.AuthService, *redis.Client) {
		r := redis.NewClient(&redis.Options{Addr: redisAddress, MaxRetries: -1})
		require.NoError(t, r.Ping(ctx).Err())
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		epochs, err := storage.NewRedisSessionStore(r)
		require.NoError(t, err)
		proofs, err := storage.NewRedisWalletSessionStore(r)
		require.NoError(t, err)
		return service.NewAuthService("anvil-wallet-lifecycle-secret-at-least-32", nil,
			service.WithSessionStore(epochs), service.WithWalletSessionStore(proofs), service.WithRestoreEpochStore(restore),
			service.WithSignatureVerifier(verifier),
			service.WithChallengeStore(storage.NewRedisChallengeStoreWithClient(r, 5*time.Minute)),
			service.WithSIWEDomain("localhost", "http://localhost/login")), r
	}
	s1, r1 := newService()
	s2, r2 := newService()
	defer func() {
		if len(ownedKeys) > 0 {
			require.NoError(t, r1.Del(context.Background(), ownedKeys...).Err())
		}
	}()
	signer := signature.NewSignatureVerifier(zap.NewNop())
	challenge, err := s1.GenerateWalletChallenge(ctx, wallet.Hex(), anvilChainID)
	require.NoError(t, err)
	ownedKeys = append(ownedKeys, challenge.ID)
	sig, err := signer.SignMessage(challenge.Message, ownerKey)
	require.NoError(t, err)
	probeConfig := config.DefaultConfig()
	probeConfig.Web3.ChainID = anvilChainID
	probeConfig.Web3.Chains = []config.ChainConfigEntry{{ID: anvilChainID, RPCs: []string{primary.server.URL, fallback.server.URL}}}
	probe, err := authplugin.NewProfileSignatureVerifier(probeConfig, zap.NewNop())
	require.NoError(t, err)
	probeValid, err := probe.VerifySignature(ctx, wallet.Hex(), challenge.Message, sig)
	require.NoError(t, err)
	require.True(t, probeValid)
	token := concurrentWalletAdmission(t, ctx, verifier, []*service.AuthService{s1, s2}, challenge, sig)
	claims, err := s1.ParseToken(token)
	require.NoError(t, err)
	ownedKeys = append(ownedKeys, "streamgate:session:"+claims.SessionID, "streamgate:wallet-proof:v1:"+claims.SessionID)
	require.Equal(t, wallet.Hex(), claims.Subject)
	require.Equal(t, signature.AccountContract, claims.AccountKind)
	require.LessOrEqual(t, claims.ExpiresAt.Sub(claims.IssuedAt.Time), time.Minute)
	var balances []any
	require.NoError(t, nft.Call(&bind.CallOpts{Context: ctx, BlockNumber: receipt.BlockNumber}, &balances, "balanceOf", owner))
	require.Zero(t, balances[0].(*big.Int).Sign())
	balances = nil
	require.NoError(t, nft.Call(&bind.CallOpts{Context: ctx, BlockNumber: receipt.BlockNumber}, &balances, "balanceOf", wallet))
	require.Equal(t, int64(1), balances[0].(*big.Int).Int64())
	t.Run("SegmentProofCost", func(t *testing.T) {
		token = walletMediaCost(t, ctx, s1, manager, wallet.Hex(), nftAddress.Hex(), token, primary, fallback)
	})
	// Deleting the consumed challenge proves renewal uses the admitted proof,
	// not a public login replay or a second challenge consumption.
	require.NoError(t, r1.Del(ctx, challenge.ID).Err())
	next, err := s2.RefreshToken(ctx, token)
	require.NoError(t, err)
	before := primary.requests.Load() + fallback.requests.Load()
	_, err = s1.RefreshToken(ctx, token)
	require.Error(t, err)
	require.Equal(t, before, primary.requests.Load()+fallback.requests.Load(), "old JTI rejected before chain work")
	primary.down.Store(true)
	next, err = s2.RefreshToken(ctx, next)
	require.NoError(t, err)
	require.Greater(t, fallback.requests.Load(), int64(0))
	fallback.down.Store(true)
	_, err = s2.RefreshToken(ctx, next)
	require.ErrorIs(t, err, signature.ErrWalletDependency)
	fallback.down.Store(false)
	next, err = s2.RefreshToken(ctx, next)
	require.NoError(t, err)
	current, err := s2.ParseToken(next)
	require.NoError(t, err)
	receipt = transact(t, client, transactor(t, ownerKey), walletContract, "setOwner", nextOwner)
	mineUntilSafe(t, rpcURL, receipt.BlockNumber.Uint64())
	safeAt := time.Now()
	require.True(t, current.ExpiresAt.After(safeAt.Add(5*time.Second)), "expired credentials are not a valid revocation sample")
	_, err = s2.RefreshToken(ctx, next)
	require.ErrorIs(t, err, signature.ErrWalletSignature)
	probeValid, err = probe.VerifySignature(ctx, wallet.Hex(), challenge.Message, sig)
	require.NoError(t, err)
	require.False(t, probeValid)
	values, err := current.ToMapClaims()
	require.NoError(t, err)
	_, err = middleware.AuthorizeAccessClaims(ctx, middleware.AccessPolicy{SessionValidator: s2, RestoreEpochValidator: s2}, values)
	require.Error(t, err)
	require.Less(t, time.Since(safeAt), 70*time.Second)
	require.True(t, current.ExpiresAt.After(time.Now()), "rejection must precede natural token expiry")
	balances = nil
	require.NoError(t, nft.Call(&bind.CallOpts{Context: ctx, BlockNumber: receipt.BlockNumber}, &balances, "balanceOf", wallet))
	require.Equal(t, int64(1), balances[0].(*big.Int).Int64())
	// A fresh owner can log in, but removing its server proof cannot fall back
	// to an EOA session even while the epoch/JWT remain otherwise valid.
	challenge, err = s2.GenerateWalletChallenge(ctx, wallet.Hex(), anvilChainID)
	require.NoError(t, err)
	ownedKeys = append(ownedKeys, challenge.ID)
	sig, err = signer.SignMessage(challenge.Message, nextKey)
	require.NoError(t, err)
	next, err = s2.AuthenticateWithWallet(ctx, wallet.Hex(), challenge.ID, sig, anvilChainID)
	require.NoError(t, err)
	current, err = s2.ParseToken(next)
	require.NoError(t, err)
	ownedKeys = append(ownedKeys, "streamgate:session:"+current.SessionID, "streamgate:wallet-proof:v1:"+current.SessionID)
	require.NoError(t, r2.Del(ctx, "streamgate:wallet-proof:v1:"+current.SessionID).Err())
	values, err = current.ToMapClaims()
	require.NoError(t, err)
	_, err = middleware.AuthorizeAccessClaims(ctx, middleware.AccessPolicy{SessionValidator: s2, RestoreEpochValidator: s2}, values)
	require.ErrorIs(t, err, middleware.ErrAccessWalletInvalid)
}

func deployWalletOnNetwork(t *testing.T, client *ethclient.Client, rpcURL string, chainID int64, owner common.Address) (address common.Address) {
	t.Helper()
	parsed, bin := loadArtifact(t, "contracts/out/Study1271Wallet.sol/Study1271Wallet.json")
	auth, err := bind.NewKeyedTransactorWithChainID(anvilKey(t, 0), big.NewInt(chainID))
	require.NoError(t, err)
	address, tx, _, err := bind.DeployContract(auth, parsed, bin, client, owner)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	receipt, err := bind.WaitMined(ctx, client, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), receipt.Status)
	mineUntilSafe(t, rpcURL, receipt.BlockNumber.Uint64())
	return address
}

func TestCWProof_ConcurrentChainsAnvil(t *testing.T) {
	urlX, stopX := startAnvilOnChain(t, 31337)
	defer stopX()
	urlY, stopY := startAnvilOnChain(t, 31338)
	defer stopY()
	x, err := ethclient.Dial(urlX)
	require.NoError(t, err)
	defer x.Close()
	y, err := ethclient.Dial(urlY)
	require.NoError(t, err)
	defer y.Close()
	ownerA := crypto.PubkeyToAddress(anvilKey(t, 0).PublicKey)
	ownerB := crypto.PubkeyToAddress(anvilKey(t, 1).PublicKey)
	walletX := deployWalletOnNetwork(t, x, urlX, 31337, ownerA)
	walletY := deployWalletOnNetwork(t, y, urlY, 31338, ownerB)
	require.Equal(t, walletX, walletY, "same textual address must not inherit another chain's policy")
	px := newWalletRPCProxy(t, urlX)
	fx := newWalletRPCProxy(t, urlX)
	py := newWalletRPCProxy(t, urlY)
	fy := newWalletRPCProxy(t, urlY)
	clientX, err := web3.NewChainClientWithFallback([]string{px.server.URL, fx.server.URL}, 31337, zap.NewNop())
	require.NoError(t, err)
	clientY, err := web3.NewChainClientWithFallback([]string{py.server.URL, fy.server.URL}, 31338, zap.NewNop())
	require.NoError(t, err)
	manager := web3.NewMultiChainManager(zap.NewNop())
	defer manager.Close()
	manager.AddChainWithClient(31337, clientX)
	manager.AddChainWithClient(31338, clientY)
	verifier := service.NewMultiChainSignatureVerifier(zap.NewNop(), nil, manager)
	signer := signature.NewSignatureVerifier(zap.NewNop())
	encoded, err := signer.SignMessage("chain isolation", anvilKey(t, 0))
	require.NoError(t, err)
	type outcome struct {
		chain int64
		proof *signature.AccountProof
		err   error
	}
	results := make(chan outcome, 16)
	for range 8 {
		go func() {
			proof, err := verifier.VerifyWalletProof(t.Context(), 31337, "personal_sign", walletX.Hex(), "chain isolation", encoded)
			results <- outcome{31337, proof, err}
		}()
		go func() {
			proof, err := verifier.VerifyWalletProof(t.Context(), 31338, "personal_sign", walletY.Hex(), "chain isolation", encoded)
			results <- outcome{31338, proof, err}
		}()
	}
	for range 16 {
		result := <-results
		if result.chain == 31337 {
			require.NoError(t, result.err)
			require.NotNil(t, result.proof)
			require.Equal(t, int64(31337), result.proof.ChainID)
		} else {
			require.ErrorIs(t, result.err, signature.ErrWalletSignature)
			require.Nil(t, result.proof)
		}
	}
	encoded, err = signer.SignMessage("chain isolation", anvilKey(t, 1))
	require.NoError(t, err)
	proof, err := verifier.VerifyWalletProof(t.Context(), 31338, "personal_sign", walletY.Hex(), "chain isolation", encoded)
	require.NoError(t, err)
	require.Equal(t, int64(31338), proof.ChainID)
}
