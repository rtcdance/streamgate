// Command walletgrpc is an acceptance-only bridge to the real local stack.
// It never signs or writes to a chain. Credentials enter/leave private files;
// stdout contains only the RPC method/status, never tokens or signatures.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type request struct {
	Method     string `json:"method"`
	Wallet     string `json:"wallet"`
	Nonce      string `json:"nonce"`
	Message    string `json:"message"`
	Signature  string `json:"signature"`
	Token      string `json:"token"`
	Capability string `json:"capability"`
	Content    string `json:"content"`
	Segment    string `json:"segment"`
}

type result struct {
	Method string `json:"method"`
	Code   string `json:"code"`
	Data   any    `json:"data,omitempty"`
}

func main() {
	target := flag.String("target", "", "local stack gRPC host:port")
	input := flag.String("input", "", "private JSON request file")
	output := flag.String("output", "", "private JSON response file")
	serve := flag.Bool("serve", false, "serve private file commands from stdin until EOF")
	directory := flag.String("request-dir", "", "private directory for bounded file commands")
	flag.Parse()
	var err error
	if *serve {
		if *input != "" || *output != "" {
			err = fmt.Errorf("serve mode uses private filenames on stdin")
		} else {
			err = serveRequests(*target, *directory, os.Stdin, os.Stdout)
		}
	} else {
		err = run(*target, *input, *output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: wallet gRPC bridge:", err)
		os.Exit(1)
	}
}

func localTarget(target string) bool {
	host, _, err := net.SplitHostPort(target)
	local := host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "monolith" || host == "api-gateway" ||
		(strings.HasPrefix(host, "sg-w3-lab-") && !strings.Contains(host, "."))
	return err == nil && local
}

func serveRequests(target, directory string, reader io.Reader, output io.Writer) error {
	info, err := os.Lstat(directory)
	if !localTarget(target) || err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("local target and private request directory required")
	}
	if _, err := fmt.Fprintln(output, "RPC_READY"); err != nil {
		return fmt.Errorf("control output unavailable")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 4096)
	for scanner.Scan() {
		var frame struct {
			Input  string `json:"input"`
			Output string `json:"output"`
		}
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&frame) != nil {
			return fmt.Errorf("invalid bounded control frame")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return fmt.Errorf("one control frame per line required")
		}
		for _, name := range []string{frame.Input, frame.Output} {
			if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, ".json") {
				return fmt.Errorf("private JSON basenames required")
			}
		}
		if err := run(target, filepath.Join(directory, frame.Input), filepath.Join(directory, frame.Output)); err != nil {
			return err
		}
	}
	if scanner.Err() != nil {
		return fmt.Errorf("bounded control input unavailable")
	}
	return nil
}

func run(target, input, output string) error {
	if !localTarget(target) {
		return fmt.Errorf("explicit local application target required")
	}
	if input == "" || output == "" || filepath.Clean(input) == filepath.Clean(output) {
		return fmt.Errorf("distinct private input and output files required")
	}
	// The file command must never follow a symlink to another credential file.
	descriptor, err := syscall.Open(input, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("request file unavailable")
	}
	file := os.NewFile(uintptr(descriptor), input)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("request file must be private")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 20*1024+1))
	var request request
	if err != nil || len(raw) > 20*1024 || json.Unmarshal(raw, &request) != nil {
		return fmt.Errorf("request JSON is invalid or exceeds its budget")
	}
	connection, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("local gRPC client unavailable")
	}
	defer func() { _ = connection.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pairs := []string{}
	if request.Token != "" {
		pairs = append(pairs, "authorization", "Bearer "+request.Token)
	}
	if request.Capability != "" {
		pairs = append(pairs, "playback-token", request.Capability)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...))
	response, err := call(ctx, connection, request)
	if err != nil && status.Code(err).String() == "Unknown" {
		return fmt.Errorf("unsupported request or non-RPC failure")
	}
	value := result{Method: request.Method, Code: status.Code(err).String(), Data: response}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("could not encode the RPC result")
	}
	if err := privateOutput(output, data); err != nil {
		return err
	}
	fmt.Printf("RPC_OBSERVED method=%s code=%s\n", value.Method, value.Code)
	return nil
}

func call(ctx context.Context, connection *grpc.ClientConn, r request) (any, error) {
	auth := authv1.NewAuthServiceClient(connection)
	streaming := streamingv1.NewStreamingServiceClient(connection)
	switch r.Method {
	case "challenge":
		return auth.GetNonce(ctx, &authv1.GetNonceRequest{WalletAddress: r.Wallet, ChainType: "ethereum"})
	case "admit":
		return auth.VerifySignature(ctx, &authv1.VerifySignatureRequest{WalletAddress: r.Wallet, ChainType: "ethereum", Nonce: r.Nonce, Message: r.Message, Signature: r.Signature})
	case "refresh":
		return auth.RefreshToken(ctx, &authv1.RefreshTokenRequest{RefreshToken: r.Token})
	case "verify":
		return auth.VerifyToken(ctx, &authv1.VerifyTokenRequest{Token: r.Token})
	case "manifest":
		return streaming.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: r.Content})
	case "segment":
		response, err := streaming.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: r.Content, SegmentId: r.Segment})
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(response.GetData())
		return map[string]any{"size": response.GetSize(), "sha256": hex.EncodeToString(digest[:])}, nil
	default:
		return nil, fmt.Errorf("unsupported method")
	}
}

func privateOutput(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("private output directory unavailable")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".rpc-result-")
	if err != nil {
		return fmt.Errorf("private output file unavailable")
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("private output write failed")
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("private output close failed")
	}
	// Atomic publication without replacing an earlier original response.
	if err := os.Link(file.Name(), path); err != nil {
		return fmt.Errorf("original response already exists or cannot be published")
	}
	return nil
}
