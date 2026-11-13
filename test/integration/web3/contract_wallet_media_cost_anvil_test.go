//go:build anvil && integration

package web3_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type walletRPCObservation struct {
	Method         string          `json:"method"`
	Params         []any           `json:"params"`
	Result         json.RawMessage `json:"result"`
	UpstreamStatus int             `json:"upstreamStatus"`
	InputRedacted  bool            `json:"inputRedacted,omitempty"`
	InputSHA256    string          `json:"inputSHA256,omitempty"`
	InputBytes     int             `json:"inputBytes,omitempty"`
}

type walletRPCTape struct {
	mu     sync.Mutex
	events []walletRPCObservation
}

func (p *walletRPCProxy) recordRPC(proxy *httputil.ReverseProxy, w http.ResponseWriter, request *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		http.Error(w, "bounded fixture RPC request required", http.StatusBadRequest)
		return
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var event walletRPCObservation
	if json.Unmarshal(raw, &event) != nil {
		http.Error(w, "single fixture RPC request required", http.StatusBadRequest)
		return
	}
	if event.Params == nil {
		event.Params = []any{}
	}
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &envelope)
	event.Result, event.UpstreamStatus = envelope.Result, response.Code
	// eth_call contains the original login signature. Preserve its size/hash
	// and selector for measurement, never publish that calldata in CI evidence.
	if event.Method == "eth_call" && len(event.Params) > 0 {
		if call, ok := event.Params[0].(map[string]any); ok {
			for _, key := range []string{"data", "input"} {
				if encoded, ok := call[key].(string); ok && len(encoded) >= 10 {
					decoded, decodeErr := hex.DecodeString(encoded[2:])
					if decodeErr == nil {
						event.InputRedacted, event.InputSHA256, event.InputBytes = true, mediaDigest(decoded), len(decoded)
						call[key] = encoded[:10]
					}
				}
			}
		}
	}
	p.tape.mu.Lock()
	p.tape.events = append(p.tape.events, event)
	p.tape.mu.Unlock()
	for key, values := range response.Header() {
		w.Header()[key] = values
	}
	w.WriteHeader(response.Code)
	_, _ = w.Write(response.Body.Bytes())
}

func (p *walletRPCProxy) observations() []walletRPCObservation {
	p.tape.mu.Lock()
	defer p.tape.mu.Unlock()
	return append([]walletRPCObservation(nil), p.tape.events...)
}

type existingWalletChains struct{ web3.ChainManagerInterface }

// The enclosing lifecycle fixture owns these live clients.
func (m existingWalletChains) Close() {}

func (m existingWalletChains) AddChain(chain int64) error {
	_, err := m.GetClient(chain)
	return err
}

type walletMediaPolicy struct{ staticRuleResolver }

func (p walletMediaPolicy) ResolvePolicySnapshot(_ context.Context, content string) (service.PolicySnapshot, error) {
	return service.PolicySnapshot{ContentID: content, Rules: p.rules, Version: 1, VerifiedAt: time.Now()}, nil
}

func mediaDigest(raw []byte) string {
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:])
}

func walletProofCalls(events []walletRPCObservation) int {
	count := 0
	for _, event := range events {
		if event.Method == "eth_call" && len(event.Params) > 0 {
			if call, ok := event.Params[0].(map[string]any); ok {
				if call["data"] == "0x1626ba7e" || call["input"] == "0x1626ba7e" {
					count++
				}
			}
		}
	}
	return count
}

func walletMediaRouter(t *testing.T, auth *service.AuthService, gate *middleware.NFTGateConfig, media []byte) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.PlaybackTokenAuth(middleware.PlaybackTokenAuthConfig{Validate: func(ctx context.Context, encoded, content, fingerprint string) (*middleware.PlaybackIdentity, error) {
		claims, err := auth.ParsePlaybackToken(ctx, encoded, content, fingerprint)
		if err != nil {
			return nil, err
		}
		values, err := claims.ToMapClaims()
		return &middleware.PlaybackIdentity{Wallet: claims.WalletAddress, SessionID: claims.SessionID, AuthEpoch: claims.AuthEpoch,
			RestoreEpoch: claims.RestoreEpoch, Claims: values}, err
	}}, zap.NewNop()))
	router.Use(middleware.JWTAuthMiddleware(middleware.JWTAuthConfig{Secret: "anvil-wallet-lifecycle-secret-at-least-32",
		Issuer: service.AccessTokenIssuer, Audience: service.AccessTokenAudience, SessionValidator: auth, RestoreEpochValidator: auth}, zap.NewNop()))
	router.Use(middleware.NFTGateMiddleware(gate, zap.NewNop()))
	router.GET("/stream/:id/segments/:segment", func(c *gin.Context) { c.Data(http.StatusOK, "video/mp2t", media) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

// Only the exact generated media fixture is exposed. This component measures
// production gRPC authorization; real MinIO/worker playback has its own gate.
type walletMediaFile struct {
	service.SegmentStorage
	path string
}

func (s walletMediaFile) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bucket != "streamgate" || key != "streams/cw-cost/0.ts" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(s.path)
}

func walletMediaGRPC(t *testing.T, ctx context.Context, auth *service.AuthService, cfg *config.Config, path string) streamingv1.StreamingServiceClient {
	t.Helper()
	grpcCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := gateway.SetupGRPCServer(grpcCtx, cfg, zap.NewNop(), &gateway.GRPCServices{AuthService: auth, SegmentStorage: walletMediaFile{path: path}})
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	return streamingv1.NewStreamingServiceClient(connection)
}

func walletMediaCost(t *testing.T, ctx context.Context, auth *service.AuthService, manager web3.ChainManagerInterface, wallet, nft, token string, primary, fallback *walletRPCProxy) string {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Web3.ChainID = anvilChainID
	cfg.Auth.JWTSecret = "anvil-wallet-lifecycle-secret-at-least-32"
	// Reuse the actual fixture client, while refusing constructor defaults for
	// every other chain. This measurement never dials a public RPC.
	ws, err := service.NewWeb3Service(service.Web3Deps{ChainManager: existingWalletChains{manager}}, cfg, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(ws.Close)
	policy := walletMediaPolicy{staticRuleResolver{rules: []middleware.GatingRule{{ChainID: anvilChainID, ContractAddress: nft, Standard: "erc721", MinBalance: 1}}}}
	gate := &middleware.NFTGateConfig{Verifier: ws, BlockProver: ws, RuleResolver: policy, DefaultChainID: anvilChainID, BlockTag: web3.BlockTagSafe}
	authorizer := service.NewPlaybackAuthorizer(gate, policy, zap.NewNop())
	auth.SetRuleVersionReader(func(context.Context, string) (int64, error) { return 1, nil })
	defer auth.SetRuleVersionReader(nil)
	claims, err := auth.ParseToken(token)
	require.NoError(t, err)
	values, err := claims.ToMapClaims()
	require.NoError(t, err)
	_, err = middleware.AuthorizeAccessClaims(ctx, middleware.AccessPolicy{SessionValidator: auth, RestoreEpochValidator: auth}, values)
	require.NoError(t, err)
	binding, err := service.SessionBindingFromVerifiedClaims(claims.SessionID, claims.AuthEpoch, claims.RestoreEpoch, values)
	require.NoError(t, err)
	authorized, err := authorizer.AuthorizePlayback(ctx, "cw-cost", wallet, binding)
	require.NoError(t, err, "real pinned ERC-721 holding must grant this capability")
	capability, err := auth.IssueCapability(ctx, authorized, time.Minute, "")
	require.NoError(t, err)
	capClaims, err := auth.ParsePlaybackToken(ctx, capability, "cw-cost", "")
	require.NoError(t, err)
	mediaPath := filepath.Join(t.TempDir(), "fixture.ts")
	output, err := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=1",
		"-t", "1", "-c:v", "libx264", "-threads", "1", "-preset", "ultrafast", "-f", "mpegts", mediaPath).CombinedOutput()
	require.NoError(t, err, "bounded FFmpeg fixture: %s", output)
	media, err := os.ReadFile(mediaPath)
	require.NoError(t, err)
	require.Greater(t, len(media), 188)
	server := walletMediaRouter(t, auth, gate, media)
	grpcClient := walletMediaGRPC(t, ctx, auth, cfg, mediaPath)
	grpcCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token, "playback-token", capability))
	beforePrimary, beforeFallback := primary.observations(), fallback.observations()
	admission := append(append([]walletRPCObservation(nil), beforePrimary...), beforeFallback...)
	require.Positive(t, walletProofCalls(admission), "the measurement must observe actual 1271 admission calls")
	started := time.Now()
	var segments []map[string]any
	for range 100 {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream/cw-cost/segments/0.ts?playback_token="+url.QueryEscape(capability), http.NoBody)
		require.True(t, requestErr == nil, "media request construction failed; private URL redacted")
		response, requestErr := server.Client().Do(request)
		require.True(t, requestErr == nil, "media transport failed; private URL redacted")
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, int64(len(media)+1)))
		require.NoError(t, response.Body.Close())
		require.NoError(t, readErr)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Equal(t, media, raw)
		segments = append(segments, map[string]any{"transport": "http", "status": response.StatusCode, "subject": wallet, "bytes": len(raw), "sha256": mediaDigest(raw), "credentialSHA256": mediaDigest([]byte(capability))})
		grpcResponse, grpcErr := grpcClient.GetSegment(grpcCtx, &streamingv1.GetSegmentRequest{ContentId: "cw-cost", SegmentId: "0.ts"})
		require.True(t, grpcErr == nil, "actual gRPC media request failed; private metadata redacted")
		require.Equal(t, media, grpcResponse.GetData())
		require.Equal(t, int64(len(media)), grpcResponse.GetSize())
		segments = append(segments, map[string]any{"transport": "grpc", "status": http.StatusOK, "subject": wallet, "bytes": grpcResponse.GetSize(), "sha256": mediaDigest(grpcResponse.GetData()), "credentialSHA256": mediaDigest([]byte(capability))})
	}
	finished := time.Now()
	require.True(t, finished.Before(capClaims.ExpiresAt.Time), "measure within the presented capability lease")
	afterPrimary, afterFallback := primary.observations(), fallback.observations()
	segmentRPC := append(append([]walletRPCObservation{}, afterPrimary[len(beforePrimary):]...), afterFallback[len(beforeFallback):]...)
	require.Empty(t, segmentRPC, "100 HTTP + 100 gRPC valid segment deliveries must not add any RPC")
	renewed, err := auth.RefreshToken(ctx, token)
	require.NoError(t, err)
	finalPrimary, finalFallback := primary.observations(), fallback.observations()
	refreshRPC := append(append([]walletRPCObservation{}, finalPrimary[len(afterPrimary):]...), finalFallback[len(afterFallback):]...)
	require.Positive(t, walletProofCalls(refreshRPC), "refresh must really revalidate 1271 proof")
	if path := os.Getenv("W3_SEGMENT_RPC_OUTPUT"); path != "" {
		candidate, evidenceContext := os.Getenv("W3_CANDIDATE_SHA"), os.Getenv("W3_EVIDENCE_CONTEXT")
		require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{40}$`), candidate)
		require.Contains(t, []string{"CANDIDATE", "DEVELOPMENT"}, evidenceContext)
		source, readErr := os.ReadFile(filepath.Join(repoRoot(t), "test", "integration", "web3", "contract_wallet_media_cost_anvil_test.go"))
		require.NoError(t, readErr)
		report := map[string]any{"schemaVersion": 1, "candidateSHA": candidate, "evidenceContext": evidenceContext, "chainId": anvilChainID,
			"scope": "REAL_CW_SEGMENT_RPC", "originalExit": 0, "wallet": wallet, "sourceSHA256": mediaDigest(source), "rpcCallFormat": "SELECTOR_AND_INPUT_DIGEST",
			"admissionRPC": admission, "segmentRPC": segmentRPC, "refreshRPC": refreshRPC, "segments": segments,
			"lease": map[string]any{"startedAtMs": started.UnixMilli(), "finishedAtMs": finished.UnixMilli(), "capabilityExpiresAtMs": capClaims.ExpiresAt.UnixMilli(),
				"accessExpiresAtMs": claims.ExpiresAt.UnixMilli(), "capabilitySHA256": mediaDigest([]byte(capability))}}
		raw, marshalErr := json.MarshalIndent(report, "", "  ")
		require.NoError(t, marshalErr)
		for _, private := range []string{token, capability, renewed} {
			require.False(t, bytes.Contains(raw, []byte(private)), "raw credential must not enter RPC cost evidence")
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		require.NoError(t, openErr, "preserve original RPC measurement")
		_, writeErr := file.Write(append(raw, '\n'))
		require.NoError(t, file.Close())
		require.NoError(t, writeErr)
	}
	return renewed
}
