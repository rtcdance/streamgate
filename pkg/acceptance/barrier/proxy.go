package barrier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

const (
	// dropSuffix switches the response-dropping proxy on. It is a file in the
	// rendezvous directory, exactly like an armed point, so the harness turns the
	// fault on without any business HTTP entry point.
	dropSuffix = ".drop"
	// droppedSuffix records that a response really was discarded. It is written
	// AFTER the upstream answered, so its presence is evidence that the chain saw
	// the request — not that the request was merely attempted.
	droppedSuffix = ".dropped"
)

// errDroppedResponse is what a lost proxy response looks like to the caller: a
// transport-level read failure. The registry publisher's isAmbiguous classifies
// it as an UNKNOWN outcome, which is the honest reading — the node answered, the
// caller simply never saw the answer.
var errDroppedResponse = errors.New("acceptance fault: the proxy discarded the response after the node answered")

// Dropping reports whether the harness switched the named drop point on.
// Nil-safe (always false).
func (b *Barrier) Dropping(name string) bool {
	if b == nil || name == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(b.dir, name+dropSuffix))
	return err == nil
}

// Transport wraps base with the response-dropping proxy of the durable
// acceptance stack. While the named drop point is on, the response to a matching
// JSON-RPC method is forwarded upstream and then DISCARDED, so the fault lands
// after the node accepted the request instead of before it.
//
// It returns base untouched when no barrier is configured, which is every
// profile but durable: a production process can neither enable the switch nor be
// faulted through it.
func (b *Barrier) Transport(base http.RoundTripper, name, method string) http.RoundTripper {
	if b == nil || name == "" || method == "" {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &rpcDropTransport{base: base, barrier: b, name: name, method: method}
}

type rpcDropTransport struct {
	base    http.RoundTripper
	barrier *Barrier
	name    string
	method  string
}

// RoundTrip forwards the request, and discards the response exactly when the
// drop switch is on and the payload is the selected JSON-RPC method. The request
// body is buffered so the method can be read without consuming it.
func (t *rpcDropTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.barrier.Dropping(t.name) {
		return t.base.RoundTrip(req)
	}
	body, err := bufferRequestBody(req)
	if err != nil {
		return nil, err
	}
	if !callsMethod(body, t.method) {
		return t.base.RoundTrip(req)
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// Drain and close first: the fault is "the response never reached the caller",
	// so the upstream transaction must have completed before it is discarded.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if err := t.barrier.markDropped(t.name); err != nil {
		return nil, err
	}
	// The node has accepted the transaction but the caller has not been told:
	// announce the point so the harness can kill the process in this exact window
	// instead of racing the publisher's poll interval.
	if arriveErr := t.barrier.Point(t.name).Arrive(req.Context()); arriveErr != nil {
		// A point nobody armed must stay a no-op, and a released point must let the
		// caller proceed with the ambiguous outcome. Either way the fault already
		// happened; the arrival is only the rendezvous.
		t.barrier.warn(t.name, arriveErr)
	}
	return nil, &net.OpError{Op: "read", Net: "tcp", Err: errDroppedResponse}
}

// bufferRequestBody reads the request body into memory and restores it, so the
// same bytes can be forwarded upstream after the method has been inspected.
func bufferRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	data, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("fault proxy: cannot read the request body: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return data, nil
}

// callsMethod reports whether the JSON-RPC payload (single object or batch)
// contains the selected method.
func callsMethod(body []byte, method string) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '[' {
		var batch []struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			return false
		}
		for _, msg := range batch {
			if msg.Method == method {
				return true
			}
		}
		return false
	}
	var single struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return false
	}
	return single.Method == method
}

// markDropped publishes the "a response really was discarded" marker.
func (b *Barrier) markDropped(name string) error {
	marker := fmt.Sprintf("%s %d %s\n", hostname, os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	path := filepath.Join(b.dir, name+droppedSuffix)
	if err := os.WriteFile(path, []byte(marker), 0o644); err != nil {
		return fmt.Errorf("fault proxy %s: cannot publish the dropped marker: %w", name, err)
	}
	return nil
}

func (b *Barrier) warn(name string, err error) {
	if b.log != nil {
		b.log.Warn("fault proxy rendezvous was not released", zap.String("point", name), zap.Error(err))
	}
}

// Dropped reports whether this process has already discarded a response for the
// named point in this run. Nil-safe (always false).
func (b *Barrier) Dropped(name string) bool {
	if b == nil || name == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(b.dir, name+droppedSuffix))
	return err == nil
}
