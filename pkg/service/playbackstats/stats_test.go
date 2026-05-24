package playbackstats

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
	pingFn     func(ctx context.Context) error
	closeFn    func() error
}

func (m *mockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}
func (m *mockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}
func (m *mockDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}
func (m *mockDB) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

type mockResult struct {
	rowsAffected int64
	lastInsertID int64
}

func (m *mockResult) LastInsertId() (int64, error) { return m.lastInsertID, nil }
func (m *mockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

func TestNewPlaybackStatsService(t *testing.T) {
	svc := NewPlaybackStatsService(&mockDB{}, zap.NewNop())
	require.NotNil(t, svc)
	assert.Equal(t, 5*time.Second, svc.debounceDelay)
	assert.NotNil(t, svc.pending)
}

func TestPlaybackStatsService_RecordEvent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewPlaybackStatsService(nil, zap.NewNop())
		err := svc.RecordEvent(context.Background(), &models.PlaybackEvent{ContentID: "c1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("success with auto-generated fields", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		svc.debounceDelay = 1 * time.Millisecond
		event := &models.PlaybackEvent{
			ContentID:       "c1",
			WalletAddress:   "0xabc",
			EventType:       "start",
			DurationSeconds: 30,
		}
		err := svc.RecordEvent(context.Background(), event)
		require.NoError(t, err)
		assert.NotEmpty(t, event.ID)
		assert.False(t, event.CreatedAt.IsZero())
	})

	t.Run("preserves provided ID and CreatedAt", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		svc.debounceDelay = 1 * time.Millisecond
		ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		event := &models.PlaybackEvent{
			ID:        "my-id",
			ContentID: "c1",
			CreatedAt: ts,
		}
		err := svc.RecordEvent(context.Background(), event)
		require.NoError(t, err)
		assert.Equal(t, "my-id", event.ID)
		assert.Equal(t, ts, event.CreatedAt)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		err := svc.RecordEvent(context.Background(), &models.PlaybackEvent{ContentID: "c1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to record playback event")
	})
}

func TestPlaybackStatsService_GetContentStats(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewPlaybackStatsService(nil, zap.NewNop())
		_, err := svc.GetContentStats(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found returns empty stats", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		stats, err := svc.GetContentStats(context.Background(), "c1")
		require.NoError(t, err)
		assert.Equal(t, "c1", stats.ContentID)
		assert.Equal(t, 0, stats.TotalPlays)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("db error"))
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		_, err := svc.GetContentStats(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query content stats")
	})
}

func TestPlaybackStatsService_ListTopContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewPlaybackStatsService(nil, zap.NewNop())
		_, err := svc.ListTopContent(context.Background(), 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("default limit", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (stg.Rows, error) {
				assert.Equal(t, 20, args[0])
				return nil, sql.ErrNoRows
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		_, _ = svc.ListTopContent(context.Background(), 0)
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		_, err := svc.ListTopContent(context.Background(), 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list top content")
	})
}

func TestPlaybackStatsService_ScheduleAggregation(t *testing.T) {
	svc := NewPlaybackStatsService(&mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}, zap.NewNop())
	svc.debounceDelay = 1 * time.Millisecond

	svc.scheduleAggregation("c1")
	svc.debounceMu.Lock()
	_, ok1 := svc.pending["c1"]
	svc.debounceMu.Unlock()
	assert.True(t, ok1)

	svc.scheduleAggregation("c1")
	svc.debounceMu.Lock()
	_, ok2 := svc.pending["c1"]
	svc.debounceMu.Unlock()
	assert.True(t, ok2)

	time.Sleep(50 * time.Millisecond)
}

func TestPlaybackStatsService_UpdateStatsAggregation(t *testing.T) {
	t.Run("db error logs warning", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewPlaybackStatsService(db, zap.NewNop())
		assert.NotPanics(t, func() {
			svc.updateStatsAggregation("c1")
		})
	})
}
