package transcoding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// serveBytes serves a body of the given size. When declared is true the server
// sets Content-Length; when false it streams, so the size is only discoverable
// by reading — which is the case a declared-length check cannot see.
func serveBytes(t *testing.T, size int, declared bool) *httptest.Server {
	t.Helper()
	payload := strings.Repeat("a", size)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		if declared {
			w.Header().Set("Content-Length", itoa(size))
		}
		_, _ = w.Write([]byte(payload))
	}))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func newDownloadTestService(limit int64) *TranscodingService {
	return NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithLogger(zap.NewNop()), WithMaxInputBytes(limit))
}

// The default ceiling is the policy value, and a non-positive override leaves it
// in place: "no limit" is not an option.
func TestMaxInputBytes_DefaultAndOverrides(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	assert.Equal(t, DefaultMaxTranscodeInputBytes, svc.MaxInputBytes())

	svc = NewTranscodingService(nil, NewMemoryTranscodingQueue(), WithMaxInputBytes(0))
	assert.Equal(t, DefaultMaxTranscodeInputBytes, svc.MaxInputBytes(),
		"a zero override must not mean 'no limit'")

	svc = NewTranscodingService(nil, NewMemoryTranscodingQueue(), WithMaxInputBytes(2048))
	assert.Equal(t, int64(2048), svc.MaxInputBytes())
}

// A declared length over the ceiling is refused BEFORE reading, so an honest
// server never makes us buffer an oversized body.
func TestDownloadInputFile_RefusesADeclaredOversizeBody(t *testing.T) {
	srv := serveBytes(t, 4096, true)
	defer srv.Close()

	svc := newDownloadTestService(1024)
	path, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInputTooLarge)
	assert.Empty(t, path)
}

// The case the declared-length check cannot see: no Content-Length, and the body
// is over the ceiling. It must FAIL, not quietly keep the first N bytes — a
// truncated original is a corrupted transcode that reports success.
func TestDownloadInputFile_RefusesAnUndeclaredOversizeBody(t *testing.T) {
	srv := serveBytes(t, 4096, false)
	defer srv.Close()

	svc := newDownloadTestService(1024)
	path, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")

	require.Error(t, err, "an oversized undeclared body must be refused, not truncated")
	assert.ErrorIs(t, err, ErrInputTooLarge)
	assert.Empty(t, path)
}

// A body that is exactly at the ceiling is accepted: the limit is a maximum, not
// an exclusive bound.
func TestDownloadInputFile_AcceptsABodyAtTheCeiling(t *testing.T) {
	srv := serveBytes(t, 1024, true)
	defer srv.Close()

	svc := newDownloadTestService(1024)
	path, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")
	require.NoError(t, err)
	defer func() { _ = os.Remove(path) }()

	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.EqualValues(t, 1024, info.Size())
}

// One byte over the ceiling is refused.
func TestDownloadInputFile_RefusesOneByteOverTheCeiling(t *testing.T) {
	srv := serveBytes(t, 1025, true)
	defer srv.Close()

	svc := newDownloadTestService(1024)
	_, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")
	assert.ErrorIs(t, err, ErrInputTooLarge)
}

// A refused download must not leave its temp file behind.
func TestDownloadInputFile_RemovesTheTempFileOnRefusal(t *testing.T) {
	before := tempInputFiles(t)

	srv := serveBytes(t, 4096, false)
	defer srv.Close()

	svc := newDownloadTestService(1024)
	_, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")
	require.Error(t, err)

	assert.Equal(t, before, tempInputFiles(t), "a refused download must clean up after itself")
}

func tempInputFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	require.NoError(t, err)
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "streamgate-input-") {
			n++
		}
	}
	return n
}
