package testutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type TestContext struct {
	Context    context.Context
	Cancel     context.CancelFunc
	StartTime  time.Time
	TestName   string
	CleanupFns []func()
}

func NewTestContext(t *testing.T) *TestContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	return &TestContext{
		Context:    ctx,
		Cancel:     cancel,
		StartTime:  time.Now(),
		TestName:   t.Name(),
		CleanupFns: make([]func(), 0),
	}
}

func (tc *TestContext) AddCleanup(fn func()) {
	tc.CleanupFns = append(tc.CleanupFns, fn)
}

func (tc *TestContext) Cleanup() {
	for i := len(tc.CleanupFns) - 1; i >= 0; i-- {
		tc.CleanupFns[i]()
	}
	tc.Cancel()
}

func (tc *TestContext) Elapsed() time.Duration {
	return time.Since(tc.StartTime)
}

func AssertNoError(t *testing.T, err error, msgAndArgs ...interface{}) {
	if err != nil {
		t.Helper()
		t.Fatalf("Unexpected error: %v %v", err, msgAndArgs)
	}
}

func AssertError(t *testing.T, err error, msgAndArgs ...interface{}) {
	if err == nil {
		t.Helper()
		t.Fatalf("Expected error but got nil %v", msgAndArgs)
	}
}

func AssertEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	require.Equal(t, expected, actual, msgAndArgs...)
}

func AssertNotEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	require.NotEqual(t, expected, actual, msgAndArgs...)
}

func AssertNil(t *testing.T, obj interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	require.Nil(t, obj, msgAndArgs...)
}

func AssertNotNil(t *testing.T, obj interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	require.NotNil(t, obj, msgAndArgs...)
}

func AssertTrue(t *testing.T, value bool, msgAndArgs ...interface{}) {
	t.Helper()
	require.True(t, value, msgAndArgs...)
}

func AssertFalse(t *testing.T, value bool, msgAndArgs ...interface{}) {
	t.Helper()
	require.False(t, value, msgAndArgs...)
}

func AssertContains(t *testing.T, collection, element interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	require.Contains(t, collection, element, msgAndArgs...)
}

func AssertLen(t *testing.T, obj interface{}, length int, msgAndArgs ...interface{}) {
	t.Helper()
	require.Len(t, obj, length, msgAndArgs...)
}

func WaitForCondition(t *testing.T, condition func() bool, timeout, interval time.Duration, msg string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for condition: %s", msg)
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

func Retry(t *testing.T, maxAttempts int, delay time.Duration, fn func() error) error {
	t.Helper()
	var lastErr error

	for i := 0; i < maxAttempts; i++ {
		if err := fn(); err != nil {
			lastErr = err
			if i < maxAttempts-1 {
				time.Sleep(delay)
			}
		} else {
			return nil
		}
	}

	return fmt.Errorf("retry failed after %d attempts: %w", maxAttempts, lastErr)
}

func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func RandomEmail() string {
	return fmt.Sprintf("%s@example.com", RandomString(10))
}

func RandomURL() string {
	return fmt.Sprintf("https://example.com/%s", RandomString(10))
}

func RandomInt(lo, hi int) int {
	return lo + rand.Intn(hi-lo)
}

func RandomDuration(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(rand.Int63n(int64(hi-lo)))
}

func MustParseTime(t *testing.T, layout, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(layout, value)
	require.NoError(t, err, "Failed to parse time")
	return parsed
}

func MustParseDuration(t *testing.T, value string) time.Duration {
	t.Helper()
	parsed, err := time.ParseDuration(value)
	require.NoError(t, err, "Failed to parse duration")
	return parsed
}

func SkipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}
}

func SkipIfEnv(t *testing.T, envVar string) {
	if value := os.Getenv(envVar); value != "" && value != "0" && value != "false" {
		t.Skipf("Skipping test due to env var %s=%s", envVar, value)
	}
}

func RunIfEnv(t *testing.T, envVar string) {
	if value := os.Getenv(envVar); value == "" || value == "0" || value == "false" {
		t.Skipf("Skipping test due to missing env var %s", envVar)
	}
}

func TempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TempFile(t *testing.T, pattern string) string {
	t.Helper()
	file, err := os.CreateTemp("", pattern)
	require.NoError(t, err, "Failed to create temp file")
	t.Cleanup(func() {
		_ = os.Remove(file.Name())
	})
	return file.Name()
}

func CaptureOutput(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}
