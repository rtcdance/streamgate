package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type bridgeContractAuth struct {
	authv1.UnimplementedAuthServiceServer
	calls atomic.Int64
}

func (s *bridgeContractAuth) VerifyToken(_ context.Context, req *authv1.VerifyTokenRequest) (*authv1.VerifyTokenResponse, error) {
	s.calls.Add(1)
	return &authv1.VerifyTokenResponse{Valid: req.Token == "private-unit-token"}, nil
}

// This is a real TCP/client/file contract with a test auth responder, never
// evidence of application authentication, MetaMask or a public-chain session.
func TestWalletGRPCBridge_ServesPrivateFilesUntilEOF(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0o700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	api := &bridgeContractAuth{}
	authv1.RegisterAuthServiceServer(server, api)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	for _, name := range []string{"first.json", "second.json"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(`{"method":"verify","token":"private-unit-token"}`), 0o600))
	}
	var output bytes.Buffer
	frames := "{\"input\":\"first.json\",\"output\":\"first.out.json\"}\n{\"input\":\"second.json\",\"output\":\"second.out.json\"}\n"
	require.NoError(t, serveRequests(listener.Addr().String(), directory, strings.NewReader(frames), &output))
	require.Equal(t, int64(2), api.calls.Load())
	require.Equal(t, "RPC_READY\n", output.String())
	for _, name := range []string{"first.out.json", "second.out.json"} {
		path := filepath.Join(directory, name)
		raw, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		var value struct {
			Code string `json:"code"`
			Data struct {
				Valid bool `json:"valid"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &value))
		require.Equal(t, "OK", value.Code)
		require.True(t, value.Data.Valid)
		require.NotContains(t, string(raw), "private-unit-token")
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestWalletGRPCBridge_RejectsEscapesSymlinksAndNonprivateInput(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0o700))
	for _, input := range []string{"../foreign.json", "/foreign.json", "sub/foreign.json"} {
		frame, err := json.Marshal(map[string]string{"input": input, "output": "result.json"})
		require.NoError(t, err)
		require.Error(t, serveRequests("localhost:9090", directory, bytes.NewReader(frame), &bytes.Buffer{}))
	}
	path := filepath.Join(directory, "input.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"method":"verify"}`), 0o644))
	require.Error(t, run("localhost:9090", path, filepath.Join(directory, "output.json")))
	require.NoError(t, os.Chmod(path, 0o600))
	link := filepath.Join(directory, "link.json")
	require.NoError(t, os.Symlink(path, link))
	require.Error(t, run("localhost:9090", link, filepath.Join(directory, "output.json")))
	require.Error(t, serveRequests("remote.example:9090", directory, strings.NewReader(""), &bytes.Buffer{}))
	require.Error(t, serveRequests("localhost:9090", directory, strings.NewReader(strings.Repeat("x", 4097)), &bytes.Buffer{}))
}

func TestWalletGRPCBridge_PreservesOriginalOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "original.json")
	require.NoError(t, privateOutput(path, []byte("original response")))
	require.Error(t, privateOutput(path, []byte("replacement")))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "original response", string(raw))
}
