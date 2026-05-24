package solana

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewSolanaMultiClient_NoEndpoints(t *testing.T) {
	_, err := NewSolanaMultiClient(zap.NewNop(), nil)
	require.Error(t, err)
	assert.Equal(t, ErrSolanaNoEndpoints, err)
}

func TestNewSolanaMultiClient_EmptyEndpoints(t *testing.T) {
	_, err := NewSolanaMultiClient(zap.NewNop(), []string{})
	require.Error(t, err)
	assert.Equal(t, ErrSolanaNoEndpoints, err)
}

func TestNewSolanaMultiClient_BlankEndpoints(t *testing.T) {
	_, err := NewSolanaMultiClient(zap.NewNop(), []string{"", ""})
	require.Error(t, err)
	assert.Equal(t, ErrSolanaNoEndpoints, err)
}

func TestNewSolanaMultiClient_UnreachableEndpoints(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	assert.NotNil(t, client)
	client.Close()
}

func TestSolanaMultiClient_RecordSuccess(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordSuccess("http://127.0.0.1:1")

	client.mu.RLock()
	ep := client.endpoints[0]
	client.mu.RUnlock()
	assert.True(t, ep.connected)
	assert.Equal(t, 100.0, ep.score)
}

func TestSolanaMultiClient_RecordFailure(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordFailure("http://127.0.0.1:1")

	client.mu.RLock()
	ep := client.endpoints[0]
	client.mu.RUnlock()
	assert.False(t, ep.connected)
	assert.Equal(t, 75.0, ep.score)
	assert.False(t, ep.cooldown.IsZero())
}

func TestSolanaMultiClient_RecordFailure_MultipleTimes(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.RecordFailure("http://127.0.0.1:1")
	client.RecordFailure("http://127.0.0.1:1")
	client.RecordFailure("http://127.0.0.1:1")

	client.mu.RLock()
	ep := client.endpoints[0]
	client.mu.RUnlock()
	assert.Equal(t, 25.0, ep.score)
}

func TestSolanaMultiClient_RecordFailure_UnknownURL(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	assert.NotPanics(t, func() {
		client.RecordFailure("http://unknown:1234")
	})
}

func TestSolanaMultiClient_RecordSuccess_UnknownURL(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	assert.NotPanics(t, func() {
		client.RecordSuccess("http://unknown:1234")
	})
}

func TestSolanaMultiClient_Statuses(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1", "http://127.0.0.1:2"})
	require.NoError(t, err)
	defer client.Close()

	statuses := client.Statuses()
	assert.Len(t, statuses, 2)
}

func TestSolanaMultiClient_GetClient_AllUnreachable(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.GetClient(ctx)
	require.Error(t, err)
}

func TestSolanaMultiClient_BestEndpoint(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1", "http://127.0.0.1:2"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 80.0
	client.endpoints[0].cooldown = time.Time{}
	client.endpoints[1].connected = true
	client.endpoints[1].score = 90.0
	client.endpoints[1].cooldown = time.Time{}
	client.mu.Unlock()

	best := client.bestEndpoint()
	require.NotNil(t, best)
	assert.Equal(t, 90.0, best.score)
}

func TestSolanaMultiClient_BestEndpoint_NoneConnected(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	best := client.bestEndpoint()
	assert.Nil(t, best)
}

func TestSolanaMultiClient_BestEndpoint_Cooldown(t *testing.T) {
	client, err := NewSolanaMultiClient(zap.NewNop(), []string{"http://127.0.0.1:1"})
	require.NoError(t, err)
	defer client.Close()

	client.mu.Lock()
	client.endpoints[0].connected = true
	client.endpoints[0].score = 100.0
	client.endpoints[0].cooldown = time.Now().Add(1 * time.Hour)
	client.mu.Unlock()

	best := client.bestEndpoint()
	assert.Nil(t, best)
}

func TestSolanaEndpointStatus_Fields(t *testing.T) {
	status := SolanaEndpointStatus{
		URL:       "http://test:8899",
		Score:     95.0,
		Connected: true,
	}
	assert.Equal(t, "http://test:8899", status.URL)
	assert.Equal(t, 95.0, status.Score)
	assert.True(t, status.Connected)
}
