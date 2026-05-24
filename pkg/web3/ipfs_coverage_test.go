package web3

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestIPFSClient_RunWithContext_Success(t *testing.T) {
	ic := &IPFSClient{
		shell:   nil,
		logger:  zap.NewNop(),
		timeout: 5 * time.Second,
	}

	err := ic.runWithContext(context.Background(), func() error {
		return nil
	})
	assert.NoError(t, err)
}

func TestIPFSClient_RunWithContext_FunctionError(t *testing.T) {
	ic := &IPFSClient{
		shell:   nil,
		logger:  zap.NewNop(),
		timeout: 5 * time.Second,
	}

	err := ic.runWithContext(context.Background(), func() error {
		return errors.New("function failed")
	})
	assert.Error(t, err)
	assert.Equal(t, "function failed", err.Error())
}

func TestIPFSClient_RunWithContext_ContextCancelled(t *testing.T) {
	ic := &IPFSClient{
		shell:   nil,
		logger:  zap.NewNop(),
		timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ic.runWithContext(ctx, func() error {
		time.Sleep(10 * time.Second)
		return nil
	})
	assert.Error(t, err)
}

func TestIPFSClient_RunWithContext_Timeout(t *testing.T) {
	ic := &IPFSClient{
		shell:   nil,
		logger:  zap.NewNop(),
		timeout: 50 * time.Millisecond,
	}

	err := ic.runWithContext(context.Background(), func() error {
		time.Sleep(5 * time.Second)
		return nil
	})
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestIPFSClient_RunWithContext_ShorterDeadline(t *testing.T) {
	ic := &IPFSClient{
		shell:   nil,
		logger:  zap.NewNop(),
		timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := ic.runWithContext(ctx, func() error {
		time.Sleep(5 * time.Second)
		return nil
	})
	assert.Error(t, err)
}

func TestHybridStorage_Store_IPFSNoClient(t *testing.T) {
	hs := NewHybridStorage(nil, zap.NewNop(), 10)

	location, err := hs.Store(context.Background(), "big.txt", make([]byte, 100))
	requireError(t, err)
	assert.Contains(t, err.Error(), "IPFS client not configured")
	assert.Nil(t, location)
}

func TestIPFSClient_DefaultTimeout(t *testing.T) {
	assert.Equal(t, 30*time.Second, defaultIPFSTimeout)
}

func requireError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
