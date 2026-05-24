package web3

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWSAvailable_TableDriven(t *testing.T) {
	tests := []struct {
		name      string
		rpcURL    string
		wantWS    string
		wantAvail bool
	}{
		{"wss scheme", "wss://rpc.example.com", "wss://rpc.example.com", true},
		{"ws scheme", "ws://localhost:8546", "ws://localhost:8546", true},
		{"https scheme", "https://rpc.example.com", "wss://rpc.example.com", true},
		{"http scheme", "http://localhost:8545", "ws://localhost:8545", true},
		{"ftp scheme", "ftp://example.com", "", false},
		{"no scheme", "just-a-host", "", false},
		{"empty string", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wsURL, avail := WSAvailable(tc.rpcURL)
			assert.Equal(t, tc.wantAvail, avail)
			assert.Equal(t, tc.wantWS, wsURL)
		})
	}
}

func TestNewLogSubscriber(t *testing.T) {
	ls := NewLogSubscriber("wss://rpc.example.com", zap.NewNop())
	require.NotNil(t, ls)
	assert.Equal(t, "wss://rpc.example.com", ls.wsURL)
	assert.NotNil(t, ls.logs)
	assert.NotNil(t, ls.reconnectC)
}

func TestLogSubscriber_Unsubscribe_NotRunning(t *testing.T) {
	ls := NewLogSubscriber("wss://rpc.example.com", zap.NewNop())
	assert.NotPanics(t, func() {
		ls.Unsubscribe()
	})
}

func TestLogSubscriber_Subscribe_AlreadyRunning(t *testing.T) {
	ls := NewLogSubscriber("wss://rpc.example.com", zap.NewNop())
	ls.running = true

	logs, err := ls.Subscribe(context.Background(), ethereum.FilterQuery{})
	assert.NoError(t, err)
	assert.NotNil(t, logs)
}

func TestLogSubscriber_Subscribe_DialFails(t *testing.T) {
	ls := NewLogSubscriber("ws://invalid-host-that-does-not-exist:9999", zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ls.Subscribe(ctx, ethereum.FilterQuery{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ws subscription failed")
}

func TestLogSubscriber_Unsubscribe_Running(t *testing.T) {
	ls := NewLogSubscriber("wss://rpc.example.com", zap.NewNop())
	ls.running = true
	ls.cancel = func() {}
	ls.Unsubscribe()
	assert.False(t, ls.running)
}
