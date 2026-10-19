package barrier

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const sendRawTxBody = `{"jsonrpc":"2.0","id":1,"method":"eth_sendRawTransaction","params":["0xdead"]}`

func okResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://anvil:8545",
		io.NopCloser(bytes.NewReader([]byte(body))))
	require.NoError(t, err)
	return req
}

// The proxy is inert unless the harness switched the drop point on: a normal
// deployment must never lose a response.
func TestProxyTransport_PassesThroughWhileTheSwitchIsOff(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(durableConfig(dir, 2*time.Second), zap.NewNop())
	require.NoError(t, err)

	var seen string
	upstream := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, rerr := io.ReadAll(r.Body)
		require.NoError(t, rerr)
		seen = string(raw)
		return okResponse(), nil
	})
	transport := b.Transport(upstream, "registry.broadcast", "eth_sendRawTransaction")

	resp, err := transport.RoundTrip(jsonRequest(t, sendRawTxBody))
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
	assert.Equal(t, sendRawTxBody, seen, "the request body must reach the node untouched")
	assert.False(t, b.Dropped("registry.broadcast"))
}

// The fault must land AFTER the node answered: the request is forwarded, the
// response is thrown away, and the caller sees an ambiguous transport failure.
func TestProxyTransport_DropsTheResponseAfterForwardingIt(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(durableConfig(dir, 5*time.Second), zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, b.Arm("registry.broadcast"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.broadcast"+dropSuffix), []byte("on\n"), 0o644))

	var seen = make(chan string, 1)
	upstream := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, rerr := io.ReadAll(r.Body)
		require.NoError(t, rerr)
		seen <- string(raw)
		return okResponse(), nil
	})
	transport := b.Transport(upstream, "registry.broadcast", "eth_sendRawTransaction")

	type outcome struct {
		resp *http.Response
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		resp, rerr := transport.RoundTrip(jsonRequest(t, sendRawTxBody))
		done <- outcome{resp: resp, err: rerr}
	}()

	// The point blocks until the harness releases it: the crash window is exactly
	// "the node accepted the tx, the caller was not told yet".
	require.Eventually(t, func() bool { return b.Point("registry.broadcast").Reached() }, 2*time.Second, 10*time.Millisecond)
	assert.True(t, b.Dropped("registry.broadcast"))
	select {
	case got := <-seen:
		assert.Equal(t, sendRawTxBody, got, "the node must have received the transaction before the response is dropped")
	case <-time.After(2 * time.Second):
		t.Fatal("the upstream never saw the transaction")
	}
	require.NoError(t, b.Point("registry.broadcast").Release())

	select {
	case got := <-done:
		require.Error(t, got.err)
		assert.Nil(t, got.resp)
		// The publisher's isAmbiguous must classify this as an unknown outcome, so
		// the nonce is never rolled back and no new signature is produced.
		var netErr net.Error
		assert.True(t, errors.As(got.err, &netErr), "the dropped response must look like a transport failure: %v", got.err)
		var opErr *net.OpError
		assert.True(t, errors.As(got.err, &opErr))
		assert.False(t, errors.Is(got.err, context.DeadlineExceeded))
	case <-time.After(3 * time.Second):
		t.Fatal("RoundTrip did not return after the release")
	}
}

// An unarmed drop point still drops (the switch is the fault) but must not wedge
// the caller waiting for a rendezvous nobody asked for.
func TestProxyTransport_UnarmedSwitchDropsWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(durableConfig(dir, 2*time.Second), zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.broadcast"+dropSuffix), []byte("on\n"), 0o644))

	transport := b.Transport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return okResponse(), nil
	}), "registry.broadcast", "eth_sendRawTransaction")

	resp, err := transport.RoundTrip(jsonRequest(t, sendRawTxBody))
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, b.Dropped("registry.broadcast"))
}

// Only the selected method is faulted: reads keep working while the write fault
// is armed, otherwise the scenario could not even observe the chain.
func TestProxyTransport_LeavesOtherMethodsAlone(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(durableConfig(dir, 2*time.Second), zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.broadcast"+dropSuffix), []byte("on\n"), 0o644))

	calls := 0
	transport := b.Transport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return okResponse(), nil
	}), "registry.broadcast", "eth_sendRawTransaction")

	resp, err := transport.RoundTrip(jsonRequest(t, `{"jsonrpc":"2.0","id":2,"method":"eth_getBalance","params":[]}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
	assert.Equal(t, 1, calls)
	assert.False(t, b.Dropped("registry.broadcast"))
}

// A nil barrier must hand the base transport back unchanged: a non-durable
// deployment pays nothing and cannot be faulted.
func TestTransport_ReturnsTheBaseTransportWithoutABarrier(t *testing.T) {
	var b *Barrier
	base := &countingTransport{}
	assert.Equal(t, base, b.Transport(base, "registry.broadcast", "eth_sendRawTransaction"))
}

type countingTransport struct{ calls int }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	return okResponse(), nil
}
