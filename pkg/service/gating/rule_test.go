package gating

import (
	"context"
	"database/sql"
	"errors"
	"testing"

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

func TestNewGatingRuleService(t *testing.T) {
	svc := NewGatingRuleService(&mockDB{}, zap.NewNop())
	require.NotNil(t, svc)
}

func TestGatingRuleService_CreateRule(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewGatingRuleService(nil, zap.NewNop())
		_, err := svc.CreateRule(context.Background(), &models.GatingRule{ContractAddress: "0x123"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("success with defaults", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		rule := &models.GatingRule{ContractAddress: "0x123", ContentID: "c1"}
		id, err := svc.CreateRule(context.Background(), rule)
		require.NoError(t, err)
		assert.NotEmpty(t, id)
		assert.Equal(t, "erc721", rule.Standard)
		assert.Equal(t, 1, rule.MinBalance)
	})

	t.Run("provided ID and standard", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		rule := &models.GatingRule{ID: "my-id", ContractAddress: "0x123", Standard: "erc1155", MinBalance: 5}
		id, err := svc.CreateRule(context.Background(), rule)
		require.NoError(t, err)
		assert.Equal(t, "my-id", id)
		assert.Equal(t, "erc1155", rule.Standard)
		assert.Equal(t, 5, rule.MinBalance)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		_, err := svc.CreateRule(context.Background(), &models.GatingRule{ContractAddress: "0x123"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create gating rule")
	})
}

func TestGatingRuleService_GetRule(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewGatingRuleService(nil, zap.NewNop())
		_, err := svc.GetRule(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		_, err := svc.GetRule(context.Background(), "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gating rule not found")
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("db error"))
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		_, err := svc.GetRule(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query gating rule")
	})
}

func TestGatingRuleService_ListRulesByContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewGatingRuleService(nil, zap.NewNop())
		_, err := svc.ListRulesByContent(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		_, err := svc.ListRulesByContent(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list gating rules")
	})
}

func TestGatingRuleService_GetActiveRulesForContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewGatingRuleService(nil, zap.NewNop())
		_, err := svc.GetActiveRulesForContent(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		_, err := svc.GetActiveRulesForContent(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query active gating rules")
	})
}

func TestGatingRuleService_UpdateRule(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewGatingRuleService(nil, zap.NewNop())
		err := svc.UpdateRule(context.Background(), &models.GatingRule{ID: "id"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 0}, nil
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		err := svc.UpdateRule(context.Background(), &models.GatingRule{ID: "missing"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gating rule not found")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 1}, nil
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		err := svc.UpdateRule(context.Background(), &models.GatingRule{ID: "r1", ContractAddress: "0xabc"})
		require.NoError(t, err)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		err := svc.UpdateRule(context.Background(), &models.GatingRule{ID: "r1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update gating rule")
	})
}

func TestGatingRuleService_DeleteRule(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewGatingRuleService(nil, zap.NewNop())
		err := svc.DeleteRule(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 0}, nil
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		err := svc.DeleteRule(context.Background(), "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gating rule not found")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 1}, nil
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		err := svc.DeleteRule(context.Background(), "r1")
		require.NoError(t, err)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewGatingRuleService(db, zap.NewNop())
		err := svc.DeleteRule(context.Background(), "r1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete gating rule")
	})
}
