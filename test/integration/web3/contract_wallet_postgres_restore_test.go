//go:build anvil && integration && fullchain

package web3_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-redis/redis/v8"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The controller performs actual pg_dump/psql restore and epoch rotation while
// this test is paused. Only a still-unexpired, real ERC-1271 admission is a
// valid recovery sample; consumed challenges and Redis proofs are retained.
func TestCWSession_RealPostgresRestore(t *testing.T) {
	dsn, redisAddress, barrier := os.Getenv("STREAMGATE_TEST_DB_DSN"), os.Getenv("W3_REDIS_ADDR"), os.Getenv("W3_RESTORE_BARRIER")
	require.NotEmpty(t, dsn, "the dedicated scratch Postgres is required")
	require.NotEmpty(t, redisAddress, "real Redis is required")
	require.NotEmpty(t, barrier, "the restore controller barrier is required")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS auth_control_state (id SMALLINT PRIMARY KEY CHECK (id = 1), restore_epoch UUID NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	authority := storage.NewPostgresRestoreEpochStore(db)
	before, err := authority.Rotate(ctx)
	require.NoError(t, err)
	rpcURL, stop := startAnvil(t)
	defer stop()
	client, err := ethclient.Dial(rpcURL)
	require.NoError(t, err)
	defer client.Close()
	key := anvilKey(t, 0)
	owner := crypto.PubkeyToAddress(key.PublicKey)
	parsed, bytecode := loadArtifact(t, "contracts/out/Study1271Wallet.sol/Study1271Wallet.json")
	wallet, _ := deployWalletFixture(t, client, rpcURL, parsed, bytecode, owner)
	primary, fallback := newWalletRPCProxy(t, rpcURL), newWalletRPCProxy(t, rpcURL)
	chain, err := web3.NewChainClientWithFallback([]string{primary.server.URL, fallback.server.URL}, anvilChainID, zap.NewNop())
	require.NoError(t, err)
	manager := web3.NewMultiChainManager(zap.NewNop())
	manager.AddChainWithClient(anvilChainID, chain)
	defer manager.Close()
	var owned []string
	newInstance := func() (*service.AuthService, *redis.Client) {
		r := redis.NewClient(&redis.Options{Addr: redisAddress, MaxRetries: -1})
		require.NoError(t, r.Ping(ctx).Err())
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		epochs, err := storage.NewRedisSessionStore(r)
		require.NoError(t, err)
		proofs, err := storage.NewRedisWalletSessionStore(r)
		require.NoError(t, err)
		return service.NewAuthService("postgres-restore-wallet-secret-at-least-32", nil,
			service.WithSessionStore(epochs), service.WithWalletSessionStore(proofs), service.WithRestoreEpochStore(authority),
			service.WithSignatureVerifier(service.NewMultiChainSignatureVerifier(zap.NewNop(), nil, manager)),
			service.WithChallengeStore(storage.NewRedisChallengeStoreWithClient(r, time.Minute)),
			service.WithSIWEDomain("localhost", "http://localhost/login")), r
	}
	s1, r1 := newInstance()
	t.Cleanup(func() {
		if len(owned) > 0 {
			require.NoError(t, r1.Del(context.Background(), owned...).Err())
		}
	})
	login := func(s *service.AuthService) (string, *service.Claims) {
		challenge, err := s.GenerateWalletChallenge(ctx, wallet.Hex(), anvilChainID)
		require.NoError(t, err)
		owned = append(owned, challenge.ID)
		encoded, err := signature.NewSignatureVerifier(zap.NewNop()).SignMessage(challenge.Message, key)
		require.NoError(t, err)
		token, err := s.AuthenticateWithWallet(ctx, wallet.Hex(), challenge.ID, encoded, anvilChainID)
		require.NoError(t, err)
		claims, err := s.ParseToken(token)
		require.NoError(t, err)
		owned = append(owned, "streamgate:session:"+claims.SessionID, "streamgate:wallet-proof:v1:"+claims.SessionID)
		return token, claims
	}
	token, claims := login(s1)
	require.Equal(t, "erc1271", claims.AccountKind)
	require.Equal(t, before.Epoch, claims.RestoreEpoch)
	s2, _ := newInstance()
	valid, err := s2.VerifyToken(ctx, token)
	require.NoError(t, err)
	require.True(t, valid.Valid, "a new instance must read the original proof, without signing again")
	refreshed := restartCWSessionInChild(t, ctx, barrier, token, []string{primary.server.URL, fallback.server.URL})
	require.True(t, refreshed != "" && refreshed != token, "a separate process must renew the original admission")
	claims, err = s2.ParseToken(refreshed)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(barrier, 0o700))
	ready, err := json.Marshal(map[string]any{"phase": "READY", "oldEpoch": before.Epoch, "tokenExpiresAt": claims.ExpiresAt.Unix(), "chainId": anvilChainID})
	require.NoError(t, err)
	readyPath := filepath.Join(barrier, "ready.json")
	require.NoError(t, os.WriteFile(readyPath+".pending", ready, 0o600))
	require.NoError(t, os.Rename(readyPath+".pending", readyPath))
	deadline := time.Now().Add(45 * time.Second)
	var done []byte
	for time.Now().Before(deadline) {
		done, err = os.ReadFile(filepath.Join(barrier, "done.json"))
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.NoError(t, err, "controller did not complete the real dump/restore within the live lease")
	var restored struct {
		OK       bool   `json:"ok"`
		OldEpoch string `json:"oldEpoch"`
		NewEpoch string `json:"newEpoch"`
	}
	require.NoError(t, json.Unmarshal(done, &restored))
	require.True(t, restored.OK)
	require.Equal(t, before.Epoch, restored.OldEpoch)
	require.NotEmpty(t, restored.NewEpoch)
	require.NotEqual(t, before.Epoch, restored.NewEpoch)
	after, err := authority.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, restored.NewEpoch, after.Epoch)
	require.True(t, claims.ExpiresAt.After(time.Now().Add(5*time.Second)), "natural expiration cannot count as recovery revocation")
	s3, r3 := newInstance()
	proofs, err := storage.NewRedisWalletSessionStore(r3)
	require.NoError(t, err)
	_, err = proofs.Load(ctx, claims.SessionID)
	require.NoError(t, err, "the original proof must still exist, isolating restore epoch revocation")
	_, err = s3.VerifyToken(ctx, refreshed)
	require.Error(t, err)
	_, err = s3.RefreshToken(ctx, refreshed)
	require.Error(t, err)
	values, err := claims.ToMapClaims()
	require.NoError(t, err)
	_, err = middleware.AuthorizeAccessClaims(ctx, middleware.AccessPolicy{SessionValidator: s3, RestoreEpochValidator: s3}, values)
	require.Error(t, err)
	fresh, freshClaims := login(s3)
	require.Equal(t, after.Epoch, freshClaims.RestoreEpoch)
	valid, err = s3.VerifyToken(ctx, fresh)
	require.NoError(t, err)
	require.True(t, valid.Valid)
	t.Log("CW_POSTGRES_RESTORE_PASS: real C, durable proof present, old live admission denied, fresh admission valid")
}

type cwRestartInput struct {
	Token   string   `json:"token"`
	RPCURLs []string `json:"rpcURLs"`
}

func restartCWSessionInChild(t *testing.T, ctx context.Context, directory, token string, urls []string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(directory, 0o700))
	input, output := filepath.Join(directory, "restart-input.json"), filepath.Join(directory, "restart-output.jwt")
	data, err := json.Marshal(cwRestartInput{Token: token, RPCURLs: urls})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(input, data, 0o600))
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCWSession_RestartChild$", "-test.timeout=30s")
	command.Env = append(os.Environ(), "W3_CW_RESTART_INPUT="+input, "W3_CW_RESTART_OUTPUT="+output)
	log, err := command.CombinedOutput()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "restart-child.log"), log, 0o600))
	require.NoError(t, err, "separate-process proof recovery failed; private child log retained")
	t.Log("CW_RESTART_CHILD_EXIT=0: original Redis proof renewed in a separate OS process without resigning")
	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	return string(raw)
}

// Subprocess fixture, invoked only by the required parent case above. It
// receives a client token and RPC locations, never the original in-memory proof
// or an owner key; the server proof must come from the real Redis record.
func TestCWSession_RestartChild(t *testing.T) {
	input := os.Getenv("W3_CW_RESTART_INPUT")
	if input == "" {
		return
	}
	raw, err := os.ReadFile(input)
	require.NoError(t, err)
	var request cwRestartInput
	require.NoError(t, json.Unmarshal(raw, &request))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: os.Getenv("STREAMGATE_TEST_DB_DSN")})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	r := redis.NewClient(&redis.Options{Addr: os.Getenv("W3_REDIS_ADDR"), MaxRetries: -1})
	defer func() { require.NoError(t, r.Close()) }()
	epochs, err := storage.NewRedisSessionStore(r)
	require.NoError(t, err)
	proofs, err := storage.NewRedisWalletSessionStore(r)
	require.NoError(t, err)
	chain, err := web3.NewChainClientWithFallback(request.RPCURLs, anvilChainID, zap.NewNop())
	require.NoError(t, err)
	manager := web3.NewMultiChainManager(zap.NewNop())
	manager.AddChainWithClient(anvilChainID, chain)
	defer manager.Close()
	svc := service.NewAuthService("postgres-restore-wallet-secret-at-least-32", nil,
		service.WithSessionStore(epochs), service.WithWalletSessionStore(proofs), service.WithRestoreEpochStore(storage.NewPostgresRestoreEpochStore(db)),
		service.WithSignatureVerifier(service.NewMultiChainSignatureVerifier(zap.NewNop(), nil, manager)))
	valid, err := svc.VerifyToken(ctx, request.Token)
	require.NoError(t, err)
	require.True(t, valid.Valid)
	renewed, err := svc.RefreshToken(ctx, request.Token)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("W3_CW_RESTART_OUTPUT"), []byte(renewed), 0o600))
}
