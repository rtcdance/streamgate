package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewPostgresDB(t *testing.T) {
	pdb := NewPostgresDB()
	assert.NotNil(t, pdb)
	assert.Nil(t, pdb.db)
}

func TestNewPostgresDBFromDB_Nil(t *testing.T) {
	pdb := NewPostgresDBFromDB(nil)
	assert.NotNil(t, pdb)
	assert.Nil(t, pdb.db)
}

func TestNewPostgresDBFromDB_RealDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	assert.NotNil(t, pdb)
	assert.Equal(t, db, pdb.DB())
}

func TestPostgresDB_Query_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	_, err := pdb.Query(context.Background(), "SELECT 1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not connected")
}

func TestPostgresDB_QueryRow_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	row := pdb.QueryRow(context.Background(), "SELECT 1")
	assert.Error(t, row.Scan())
}

func TestPostgresDB_Exec_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	_, err := pdb.Exec(context.Background(), "SELECT 1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not connected")
}

func TestPostgresDB_Begin_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	_, err := pdb.Begin(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not connected")
}

func TestPostgresDB_InTransaction_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	err := pdb.InTransaction(context.Background(), func(tx *sql.Tx) error { return nil })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not connected")
}

func TestPostgresDB_Ping_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	err := pdb.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not connected")
}

func TestPostgresDB_Close_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	err := pdb.Close()
	assert.NoError(t, err)
}

func TestPostgresDB_Reconnect_NoDSN(t *testing.T) {
	pdb := NewPostgresDB()
	err := pdb.Reconnect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no DSN configured")
}

func TestPostgresDB_Stats_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	stats := pdb.Stats()
	assert.Equal(t, 0, stats.MaxOpenConnections)
}

func TestPostgresDB_DB_NotConnected(t *testing.T) {
	pdb := NewPostgresDB()
	assert.Nil(t, pdb.DB())
}

func TestPostgresDB_SetMaxOpenConns_NilDB(t *testing.T) {
	pdb := NewPostgresDB()
	assert.NotPanics(t, func() { pdb.SetMaxOpenConns(10) })
}

func TestPostgresDB_SetMaxIdleConns_NilDB(t *testing.T) {
	pdb := NewPostgresDB()
	assert.NotPanics(t, func() { pdb.SetMaxIdleConns(5) })
}

func TestPostgresDB_SetConnMaxLifetime_NilDB(t *testing.T) {
	pdb := NewPostgresDB()
	assert.NotPanics(t, func() { pdb.SetConnMaxLifetime(time.Minute) })
}

func TestPostgresDB_SetConnMaxIdleTime_NilDB(t *testing.T) {
	pdb := NewPostgresDB()
	assert.NotPanics(t, func() { pdb.SetConnMaxIdleTime(time.Minute) })
}

func TestNewErrorCancelRow(t *testing.T) {
	err := context.Canceled
	cr := NewErrorCancelRow(err)
	assert.Error(t, cr.Scan())
}

func TestPoolConfigFromValues(t *testing.T) {
	cfg := PoolConfigFromValues(10, 5, time.Minute, 30*time.Second)
	assert.Equal(t, 10, cfg.MaxOpenConns)
	assert.Equal(t, 5, cfg.MaxIdleConns)
	assert.Equal(t, time.Minute, cfg.ConnMaxLifetime)
	assert.Equal(t, 30*time.Second, cfg.ConnMaxIdleTime)
}

func TestPostgresDB_SetCircuitBreaker(t *testing.T) {
	pdb := NewPostgresDB()
	assert.NotPanics(t, func() { pdb.SetCircuitBreaker(nil) })
}

func newOpenCircuitBreaker() *resilience.CircuitBreaker {
	cb := resilience.NewCircuitBreaker("test", resilience.CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          30 * time.Second,
	}, zap.NewNop())
	cb.RecordFailure()
	return cb
}

func TestPostgresDB_CircuitBreaker_Open_BlocksQuery(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetCircuitBreaker(newOpenCircuitBreaker())

	_, err = pdb.Query(context.Background(), "SELECT 1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestPostgresDB_CircuitBreaker_Open_BlocksExec(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetCircuitBreaker(newOpenCircuitBreaker())

	_, err = pdb.Exec(context.Background(), "SELECT 1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestPostgresDB_CircuitBreaker_Open_BlocksQueryRow(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetCircuitBreaker(newOpenCircuitBreaker())

	row := pdb.QueryRow(context.Background(), "SELECT 1")
	err = row.Scan()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestPostgresDB_CircuitBreaker_Open_BlocksPing(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetCircuitBreaker(newOpenCircuitBreaker())

	err = pdb.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestPostgresDB_CircuitBreaker_Open_BlocksInTransaction(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetCircuitBreaker(newOpenCircuitBreaker())

	err = pdb.InTransaction(context.Background(), func(tx *sql.Tx) error { return nil })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestPoolConfig_Apply_CustomValues(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	cfg := PoolConfig{
		MaxOpenConns:    20,
		MaxIdleConns:    10,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	}
	cfg.apply(db)

	stats := db.Stats()
	assert.Equal(t, 20, stats.MaxOpenConnections)
}

func TestPoolConfig_Apply_Defaults(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	cfg := PoolConfig{}
	cfg.apply(db)

	stats := db.Stats()
	assert.Equal(t, defaultMaxOpenConns, stats.MaxOpenConnections)
}

type errRowScanner struct{}

func (e *errRowScanner) Scan(dest ...interface{}) error {
	return errors.New("mock scan error")
}

func TestCancelRow_NewCancelRow(t *testing.T) {
	cr := NewTestCancelRow(&errRowScanner{})
	assert.NotNil(t, cr)
	err := cr.Scan()
	assert.ErrorContains(t, err, "mock scan error")
}

func TestCancelRow_Scan_WithPreError(t *testing.T) {
	expectedErr := errors.New("pre-existing error")
	cr := NewErrorCancelRow(expectedErr)
	err := cr.Scan()
	assert.Equal(t, expectedErr, err)
}

func TestCancelRow_CancelCalledOnScan(t *testing.T) {
	cancelCalled := false
	cr := &CancelRow{
		cancel: func() { cancelCalled = true },
		err:    errors.New("test error"),
	}
	_ = cr.Scan()
	assert.True(t, cancelCalled)
}

func TestPostgresDB_SetMaxOpenConns_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetMaxOpenConns(15)
	stats := pdb.Stats()
	assert.Equal(t, 15, stats.MaxOpenConnections)
}

func TestPostgresDB_SetMaxIdleConns_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	assert.NotPanics(t, func() { pdb.SetMaxIdleConns(7) })
}

func TestPostgresDB_SetConnMaxLifetime_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	assert.NotPanics(t, func() { pdb.SetConnMaxLifetime(10 * time.Minute) })
}

func TestPostgresDB_SetConnMaxIdleTime_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	assert.NotPanics(t, func() { pdb.SetConnMaxIdleTime(3 * time.Minute) })
}

func TestPostgresDB_Stats_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	stats := pdb.Stats()
	assert.Equal(t, 0, stats.MaxOpenConnections)
}

func TestPostgresDB_Query_WithContextDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = pdb.Query(ctx, "SELECT 1")
	assert.Error(t, err)
}

func TestPostgresDB_QueryRow_WithContextDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	row := pdb.QueryRow(ctx, "SELECT 1")
	assert.Error(t, row.Scan())
}

func TestPostgresDB_Exec_WithContextDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = pdb.Exec(ctx, "SELECT 1")
	assert.Error(t, err)
}

func TestPostgresDB_Ping_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	err = pdb.Ping(context.Background())
	assert.Error(t, err)
}

func TestPostgresDB_Begin_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	_, err = pdb.Begin(context.Background())
	assert.Error(t, err)
}

func TestPostgresDB_InTransaction_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	err = pdb.InTransaction(context.Background(), func(tx *sql.Tx) error { return nil })
	assert.Error(t, err)
}

func TestPostgresDB_CircuitBreaker_RecordsFailure(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_, _ = pdb.Exec(context.Background(), "SELECT 1")

	stats := cb.Stats()
	assert.Equal(t, 1, stats.FailureCount)
}

func TestPostgresDB_CircuitBreaker_RecordsSuccess(t *testing.T) {
	pdb := NewPostgresDB()
	pdb.db = nil

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_, _ = pdb.Exec(context.Background(), "SELECT 1")

	stats := cb.Stats()
	assert.Equal(t, 0, stats.SuccessCount)
}

func TestPostgresDB_Close_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)

	pdb := NewPostgresDBFromDB(db)
	err = pdb.Close()
	assert.NoError(t, err)
}

func TestPostgresDB_Reconnect_WithDSN(t *testing.T) {
	pdb := NewPostgresDB()
	pdb.dsn = "host=localhost port=1 dbname=test sslmode=disable"

	err := pdb.Reconnect(context.Background())
	assert.Error(t, err)
}

func TestPostgresDB_Query_NoDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	_, err = pdb.Query(context.Background(), "SELECT 1")
	assert.Error(t, err)
}

func TestPostgresDB_Exec_NoDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	_, err = pdb.Exec(context.Background(), "SELECT 1")
	assert.Error(t, err)
}

func TestPostgresDB_QueryRow_NoDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	row := pdb.QueryRow(context.Background(), "SELECT 1")
	assert.Error(t, row.Scan())
}

func TestPostgresDB_InTransaction_BeginTxFails(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslspace=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	err = pdb.InTransaction(context.Background(), func(tx *sql.Tx) error { return nil })
	assert.Error(t, err)
}

func TestPostgresDB_InTransaction_WithDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = pdb.InTransaction(ctx, func(tx *sql.Tx) error { return nil })
	assert.Error(t, err)
}

func TestPostgresDB_CircuitBreaker_Open_BlocksBegin(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetCircuitBreaker(newOpenCircuitBreaker())

	_, err = pdb.Begin(context.Background())
	assert.Error(t, err)
}

func TestPostgresDB_Exec_RecordsSuccess(t *testing.T) {
	pdb := NewPostgresDB()
	pdb.db = nil

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_, _ = pdb.Exec(context.Background(), "SELECT 1")

	stats := cb.Stats()
	assert.Equal(t, 0, stats.SuccessCount)
}

func TestPostgresDB_Query_RecordsFailure(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_, _ = pdb.Query(context.Background(), "SELECT 1")

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.FailureCount, 1)
}

func TestPostgresDB_QueryRow_RecordsFailure(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_ = pdb.QueryRow(context.Background(), "SELECT 1").Scan()
}

func TestPostgresDB_Ping_RecordsFailure(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_ = pdb.Ping(context.Background())

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.FailureCount, 1)
}

func TestPostgresDB_InTransaction_RecordsFailure(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_ = pdb.InTransaction(context.Background(), func(tx *sql.Tx) error { return nil })

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.FailureCount, 1)
}

func TestPostgresDB_ConnectWithConfig_Failure(t *testing.T) {
	pdb := NewPostgresDB()
	err := pdb.ConnectWithConfig("host=localhost port=1 dbname=test sslmode=disable", PoolConfig{})
	assert.Error(t, err)
}

func TestPostgresDB_Connect_Failure(t *testing.T) {
	pdb := NewPostgresDB()
	err := pdb.Connect("host=localhost port=1 dbname=test sslmode=disable")
	assert.Error(t, err)
}

func TestPostgresDB_Reconnect_WithExistingDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)

	pdb := NewPostgresDBFromDB(db)
	pdb.dsn = "host=localhost port=1 dbname=test sslmode=disable"

	err = pdb.Reconnect(context.Background())
	assert.Error(t, err)
}

func TestPostgresDB_Constants(t *testing.T) {
	assert.Equal(t, 3, defaultMaxRetries)
	assert.Equal(t, time.Second, defaultRetryBackoff)
	assert.Equal(t, 25, defaultMaxOpenConns)
	assert.Equal(t, 12, defaultMaxIdleConns)
	assert.Equal(t, 15*time.Minute, defaultConnMaxLifetime)
	assert.Equal(t, 5*time.Minute, defaultConnMaxIdleTime)
}

func TestPostgresDB_InTransaction_FnError(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	fnErr := errors.New("business logic failed")
	err = pdb.InTransaction(context.Background(), func(tx *sql.Tx) error {
		return fnErr
	})
	assert.Error(t, err)
}

func TestPostgresDB_InTransaction_PanicRecovery(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)

	err = pdb.InTransaction(context.Background(), func(tx *sql.Tx) error {
		return errors.New("fn error instead of panic")
	})
	assert.Error(t, err)
}

func TestPostgresDB_InTransaction_PanicRecovery_WithCircuitBreaker(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	err = pdb.InTransaction(context.Background(), func(tx *sql.Tx) error {
		return errors.New("fn error with cb")
	})
	assert.Error(t, err)
}

func TestPostgresDB_QueryRow_WithNoDeadline(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	row := pdb.QueryRow(context.Background(), "SELECT 1")
	assert.Error(t, row.Scan())
}

func TestPostgresDB_Begin_CircuitBreakerOpen(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetCircuitBreaker(newOpenCircuitBreaker())

	_, err = pdb.Begin(context.Background())
	assert.Error(t, err)
}

func TestPostgresDB_Query_CircuitBreakerRecordsSuccess(t *testing.T) {
	pdb := NewPostgresDB()
	pdb.db = nil

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_, _ = pdb.Query(context.Background(), "SELECT 1")

	stats := cb.Stats()
	assert.Equal(t, 0, stats.SuccessCount)
}

func TestPostgresDB_QueryRow_CircuitBreakerRecordsFailure(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_ = pdb.QueryRow(context.Background(), "SELECT 1").Scan()
}

func TestPostgresDB_Ping_CircuitBreakerRecordsSuccess(t *testing.T) {
	pdb := NewPostgresDB()
	pdb.db = nil

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	pdb.SetCircuitBreaker(cb)

	_ = pdb.Ping(context.Background())

	stats := cb.Stats()
	assert.Equal(t, 0, stats.SuccessCount)
}

func TestPostgresDB_Stats_WithRealDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	pdb.SetMaxOpenConns(30)
	stats := pdb.Stats()
	assert.Equal(t, 30, stats.MaxOpenConnections)
}

func TestPostgresDB_DB_ReturnsUnderlying(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	assert.Equal(t, db, pdb.DB())
}

func TestPostgresDB_ConnectWithConfig_CustomPoolConfig(t *testing.T) {
	pdb := NewPostgresDB()
	cfg := PoolConfig{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: 30 * time.Second,
	}
	err := pdb.ConnectWithConfig("host=localhost port=1 dbname=test sslmode=disable", cfg)
	assert.Error(t, err)
}
