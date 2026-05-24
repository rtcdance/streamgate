package category

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

func TestNewCategoryService(t *testing.T) {
	svc := NewCategoryService(&mockDB{}, zap.NewNop())
	require.NotNil(t, svc)
}

func TestCategoryService_CreateCategory(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		_, err := svc.CreateCategory(context.Background(), &models.Category{Name: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		id, err := svc.CreateCategory(context.Background(), &models.Category{Name: "test", Slug: "test"})
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	})

	t.Run("with provided ID", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		id, err := svc.CreateCategory(context.Background(), &models.Category{ID: "my-id", Name: "test", Slug: "test"})
		require.NoError(t, err)
		assert.Equal(t, "my-id", id)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		_, err := svc.CreateCategory(context.Background(), &models.Category{Name: "test", Slug: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create category")
	})
}

func TestCategoryService_GetCategory(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		_, err := svc.GetCategory(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		_, err := svc.GetCategory(context.Background(), "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "category not found")
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("db error"))
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		_, err := svc.GetCategory(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query category")
	})
}

func TestCategoryService_ListCategories(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		_, err := svc.ListCategories(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		_, err := svc.ListCategories(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list categories")
	})
}

func TestCategoryService_UpdateCategory(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		err := svc.UpdateCategory(context.Background(), &models.Category{ID: "id"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 0}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.UpdateCategory(context.Background(), &models.Category{ID: "missing"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "category not found")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 1}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.UpdateCategory(context.Background(), &models.Category{ID: "c1", Name: "updated", Slug: "updated"})
		require.NoError(t, err)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.UpdateCategory(context.Background(), &models.Category{ID: "c1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update category")
	})
}

func TestCategoryService_DeleteCategory(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		err := svc.DeleteCategory(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 0}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.DeleteCategory(context.Background(), "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "category not found")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 1}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.DeleteCategory(context.Background(), "c1")
		require.NoError(t, err)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.DeleteCategory(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete category")
	})
}

func TestCategoryService_BindContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		err := svc.BindContent(context.Background(), "content1", "cat1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.BindContent(context.Background(), "content1", "cat1")
		require.NoError(t, err)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.BindContent(context.Background(), "content1", "cat1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to bind content to category")
	})
}

func TestCategoryService_UnbindContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		err := svc.UnbindContent(context.Background(), "content1", "cat1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.UnbindContent(context.Background(), "content1", "cat1")
		require.NoError(t, err)
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		err := svc.UnbindContent(context.Background(), "content1", "cat1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unbind content from category")
	})
}

func TestCategoryService_ListContentByCategory(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewCategoryService(nil, zap.NewNop())
		_, err := svc.ListContentByCategory(context.Background(), "cat1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewCategoryService(db, zap.NewNop())
		_, err := svc.ListContentByCategory(context.Background(), "cat1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list content by category")
	})
}

func TestNilIfEmpty(t *testing.T) {
	tests := []struct {
		input string
		isNil bool
	}{
		{"", true},
		{"value", false},
	}
	for _, tt := range tests {
		result := nilIfEmpty(tt.input)
		if tt.isNil {
			assert.Nil(t, result)
		} else {
			assert.Equal(t, tt.input, result)
		}
	}
}
