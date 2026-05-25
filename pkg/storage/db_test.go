package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDB struct {
	pingErr   error
	queryFn   func(ctx context.Context, query string, args ...interface{}) (Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *CancelRow
	execFn    func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn   func(ctx context.Context) (*sql.Tx, error)
	inTxFn    func(ctx context.Context, fn func(tx *sql.Tx) error) error
}

func (m *mockDB) Query(ctx context.Context, query string, args ...interface{}) (Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, nil
}
func (m *mockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return &CancelRow{err: sql.ErrNoRows, cancel: func() {}}
}
func (m *mockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, nil
}
func (m *mockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, nil
}
func (m *mockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return nil
}
func (m *mockDB) Ping(_ context.Context) error { return m.pingErr }
func (m *mockDB) Close() error                 { return nil }

func TestNewDatabase_UnsupportedType(t *testing.T) {
	_, err := NewDatabase(DatabaseConfig{Type: "mysql"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported database type")
}

func TestNewDatabaseWithImpl(t *testing.T) {
	impl := &mockDB{}
	db := NewDatabaseWithImpl(impl, "mock")
	require.NotNil(t, db)
	assert.Equal(t, "mock", db.GetType())
}

func TestDatabase_GetType(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "test-type")
	assert.Equal(t, "test-type", db.GetType())
}

func TestDatabase_DBImpl(t *testing.T) {
	impl := &mockDB{}
	db := NewDatabaseWithImpl(impl, "mock")
	assert.Equal(t, impl, db.DBImpl())
}

func TestDatabase_GetDB_NonPostgres(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	result := db.GetDB()
	assert.Nil(t, result)
}

func TestDatabase_GetDB_Postgres(t *testing.T) {
	pgDB, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer pgDB.Close()

	pg := NewPostgresDBFromDB(pgDB)
	db := NewDatabaseWithImpl(pg, "postgres")
	result := db.GetDB()
	assert.Equal(t, pgDB, result)
}

func TestDatabase_Close(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	err := db.Close()
	assert.NoError(t, err)
}

func TestDatabase_Ping(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	err := db.Ping(context.Background())
	assert.NoError(t, err)
}

func TestDatabase_Ping_Error(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{pingErr: context.DeadlineExceeded}, "mock")
	err := db.Ping(context.Background())
	assert.Error(t, err)
}

func TestDatabase_Query(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	rows, err := db.Query(context.Background(), "SELECT 1")
	if rows != nil {
		rows.Close()
	}
	assert.NoError(t, err)
}

func TestDatabase_QueryRow(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	row := db.QueryRow(context.Background(), "SELECT 1")
	assert.Error(t, row.Scan())
}

func TestDatabase_Exec(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	_, err := db.Exec(context.Background(), "SELECT 1")
	assert.NoError(t, err)
}

func TestDatabase_Begin(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	_, err := db.Begin(context.Background())
	assert.NoError(t, err)
}

func TestDatabase_InTransaction(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	err := db.InTransaction(context.Background(), func(tx *sql.Tx) error { return nil })
	assert.NoError(t, err)
}

func TestDatabase_GetUser_QueryRowError(t *testing.T) {
	impl := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *CancelRow {
			return NewErrorCancelRow(errors.New("query failed"))
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	_, err := db.GetUser(context.Background(), "testuser")
	assert.Error(t, err)
}

func TestDatabase_GetUser_NoRows(t *testing.T) {
	impl := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *CancelRow {
			return NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	_, err := db.GetUser(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestDatabase_CreateUser_ExecError(t *testing.T) {
	impl := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("insert failed")
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	user := &models.User{
		ID:        "user-1",
		Username:  "testuser",
		Password:  "hashed",
		Email:     "test@example.com",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := db.CreateUser(context.Background(), user)
	assert.Error(t, err)
}

func TestDatabase_CreateUser_Success(t *testing.T) {
	impl := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	user := &models.User{
		ID:            "user-2",
		Username:      "newuser",
		Password:      "hashed",
		Email:         "new@example.com",
		WalletAddress: "0xABC",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	err := db.CreateUser(context.Background(), user)
	assert.NoError(t, err)
}

func TestDatabase_UpdateUser_ExecError(t *testing.T) {
	impl := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("update failed")
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	user := &models.User{
		ID:        "user-1",
		Username:  "testuser",
		Password:  "newhashed",
		UpdatedAt: time.Now(),
	}
	err := db.UpdateUser(context.Background(), user)
	assert.Error(t, err)
}

func TestDatabase_UpdateUser_Success(t *testing.T) {
	impl := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	user := &models.User{
		ID:            "user-1",
		Username:      "updateduser",
		Password:      "newhashed",
		Email:         "updated@example.com",
		WalletAddress: "0xDEF",
		UpdatedAt:     time.Now(),
	}
	err := db.UpdateUser(context.Background(), user)
	assert.NoError(t, err)
}

func TestDatabase_ImplementsUserRepository(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	var _ UserRepository = db
}

func TestDatabaseConfig_Fields(t *testing.T) {
	cfg := DatabaseConfig{
		Type:    "postgres",
		DSN:     "host=localhost dbname=test",
		PoolCfg: PoolConfig{MaxOpenConns: 25},
	}
	assert.Equal(t, "postgres", cfg.Type)
	assert.Equal(t, "host=localhost dbname=test", cfg.DSN)
	assert.Equal(t, 25, cfg.PoolCfg.MaxOpenConns)
}

func TestDatabase_Query_DelegatesToImpl(t *testing.T) {
	called := false
	impl := &mockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (Rows, error) {
			called = true
			assert.Equal(t, "SELECT 1", query)
			return nil, nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")
	rows, _ := db.Query(context.Background(), "SELECT 1")
	if rows != nil {
		rows.Close()
	}
	assert.True(t, called)
}

func TestDatabase_Exec_DelegatesToImpl(t *testing.T) {
	called := false
	impl := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			called = true
			assert.Equal(t, "DELETE FROM users", query)
			return nil, nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")
	_, _ = db.Exec(context.Background(), "DELETE FROM users")
	assert.True(t, called)
}

func TestDatabase_Begin_DelegatesToImpl(t *testing.T) {
	called := false
	impl := &mockDB{
		beginFn: func(ctx context.Context) (*sql.Tx, error) {
			called = true
			return nil, nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")
	_, _ = db.Begin(context.Background())
	assert.True(t, called)
}

func TestDatabase_InTransaction_DelegatesToImpl(t *testing.T) {
	called := false
	impl := &mockDB{
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			called = true
			return nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")
	_ = db.InTransaction(context.Background(), func(tx *sql.Tx) error { return nil })
	assert.True(t, called)
}

func TestDatabase_Close_DelegatesToImpl(t *testing.T) {
	impl := &mockDB{}
	db := NewDatabaseWithImpl(impl, "mock")
	err := db.Close()
	assert.NoError(t, err)
}

func TestDatabase_Ping_DelegatesToImpl(t *testing.T) {
	impl := &mockDB{}
	db := NewDatabaseWithImpl(impl, "mock")
	err := db.Ping(context.Background())
	assert.NoError(t, err)
}

func TestNewDatabase_PostgresConnectFails(t *testing.T) {
	_, err := NewDatabase(DatabaseConfig{
		Type: "postgres",
		DSN:  "host=localhost port=1 dbname=test sslmode=disable",
	})
	assert.Error(t, err)
}

func TestNewDatabase_PostgresqlConnectFails(t *testing.T) {
	_, err := NewDatabase(DatabaseConfig{
		Type: "postgresql",
		DSN:  "host=localhost port=1 dbname=test sslmode=disable",
	})
	assert.Error(t, err)
}

func TestDatabase_GetUser_Success(t *testing.T) {
	impl := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *CancelRow {
			return NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	_, err := db.GetUser(context.Background(), "testuser")
	assert.Error(t, err)
}

func TestDatabase_QueryRow_DelegatesToImpl(t *testing.T) {
	called := false
	impl := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *CancelRow {
			called = true
			return NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")
	_ = db.QueryRow(context.Background(), "SELECT 1")
	assert.True(t, called)
}

func TestDatabase_GetUser_ScanError(t *testing.T) {
	impl := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *CancelRow {
			return NewErrorCancelRow(errors.New("scan error"))
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	_, err := db.GetUser(context.Background(), "testuser")
	assert.Error(t, err)
}

func TestDatabase_NewDatabase_PostgresType(t *testing.T) {
	_, err := NewDatabase(DatabaseConfig{
		Type: "postgres",
		DSN:  "host=localhost port=1 dbname=test sslmode=disable",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to PostgreSQL")
}

func TestDatabase_NewDatabase_PostgresqlType(t *testing.T) {
	_, err := NewDatabase(DatabaseConfig{
		Type: "postgresql",
		DSN:  "host=localhost port=1 dbname=test sslmode=disable",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to PostgreSQL")
}

func TestDatabase_CreateUser_DelegatesToImpl(t *testing.T) {
	called := false
	impl := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			called = true
			return nil, nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	user := &models.User{
		ID:        "user-delegate",
		Username:  "testuser",
		Password:  "hashed",
		Email:     "test@example.com",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := db.CreateUser(context.Background(), user)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestDatabase_UpdateUser_DelegatesToImpl(t *testing.T) {
	called := false
	impl := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			called = true
			return nil, nil
		},
	}
	db := NewDatabaseWithImpl(impl, "mock")

	user := &models.User{
		ID:        "user-update-delegate",
		Username:  "testuser",
		Password:  "newhashed",
		UpdatedAt: time.Now(),
	}
	err := db.UpdateUser(context.Background(), user)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestDatabase_GetDB_NilImpl(t *testing.T) {
	db := NewDatabaseWithImpl(&mockDB{}, "mock")
	result := db.GetDB()
	assert.Nil(t, result)
}

func TestDatabaseConfig_Defaults(t *testing.T) {
	cfg := DatabaseConfig{}
	assert.Equal(t, "", cfg.Type)
	assert.Equal(t, "", cfg.DSN)
}
