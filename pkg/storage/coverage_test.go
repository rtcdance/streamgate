package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPoolConfig_Apply_MixedValues(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 user=test password=test dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	cfg := PoolConfig{
		MaxOpenConns:    10,
		MaxIdleConns:    0,
		ConnMaxLifetime: 0,
		ConnMaxIdleTime: 3 * time.Minute,
	}
	cfg.apply(db)

	stats := db.Stats()
	assert.Equal(t, 10, stats.MaxOpenConnections)
}

func TestPoolConfig_Apply_AllCustom(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 user=test password=test dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	cfg := PoolConfig{
		MaxOpenConns:    5,
		MaxIdleConns:    3,
		ConnMaxLifetime: 10 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	}
	cfg.apply(db)

	stats := db.Stats()
	assert.Equal(t, 5, stats.MaxOpenConnections)
}

func TestCancelRow_NewTestCancelRow(t *testing.T) {
	scanner := &mockScanner{scanErr: nil}
	cr := NewTestCancelRow(scanner)
	assert.NotNil(t, cr)
}

type mockScanner struct {
	scanErr error
}

func (m *mockScanner) Scan(dest ...interface{}) error {
	return m.scanErr
}

func TestCancelRow_Scan_DelegatesToRow(t *testing.T) {
	scanner := &mockScanner{scanErr: nil}
	cr := NewTestCancelRow(scanner)
	err := cr.Scan()
	assert.NoError(t, err)
}

func TestCancelRow_Scan_RowError(t *testing.T) {
	scanner := &mockScanner{scanErr: sql.ErrTxDone}
	cr := NewTestCancelRow(scanner)
	err := cr.Scan()
	assert.ErrorIs(t, err, sql.ErrTxDone)
}

func TestDatabase_GetDB_NotPostgres(t *testing.T) {
	mock := &mockDB{}
	db := NewDatabaseWithImpl(mock, "mock")
	result := db.GetDB()
	assert.Nil(t, result)
}

func TestDatabase_GetDB_PostgresImpl(t *testing.T) {
	sqlDB, err := sql.Open("postgres", "host=localhost port=1 user=test password=test dbname=test sslmode=disable")
	require.NoError(t, err)
	defer sqlDB.Close()

	pg := NewPostgresDBFromDB(sqlDB)
	db := NewDatabaseWithImpl(pg, "postgres")
	result := db.GetDB()
	assert.Equal(t, sqlDB, result)
}

func TestDatabase_Query_WithMockFn(t *testing.T) {
	mock := &mockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (Rows, error) {
			return nil, fmt.Errorf("query error")
		},
	}
	db := NewDatabaseWithImpl(mock, "mock")
	rows, err := db.Query(context.Background(), "SELECT 1")
	if rows != nil {
		rows.Close()
	}
	assert.EqualError(t, err, "query error")
}

func TestDatabase_QueryRow_WithMockFn(t *testing.T) {
	mock := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *CancelRow {
			return &CancelRow{err: sql.ErrConnDone, cancel: func() {}}
		},
	}
	db := NewDatabaseWithImpl(mock, "mock")
	row := db.QueryRow(context.Background(), "SELECT 1")
	assert.NotNil(t, row)
	assert.Error(t, row.Scan())
}

func TestDatabase_Exec_WithMockFn(t *testing.T) {
	mock := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, fmt.Errorf("exec error")
		},
	}
	db := NewDatabaseWithImpl(mock, "mock")
	_, err := db.Exec(context.Background(), "DELETE FROM x")
	assert.EqualError(t, err, "exec error")
}

func TestDatabase_Begin_WithMockFn(t *testing.T) {
	mock := &mockDB{
		beginFn: func(ctx context.Context) (*sql.Tx, error) {
			return nil, fmt.Errorf("begin error")
		},
	}
	db := NewDatabaseWithImpl(mock, "mock")
	_, err := db.Begin(context.Background())
	assert.EqualError(t, err, "begin error")
}

func TestDatabase_InTransaction_WithMockFn(t *testing.T) {
	mock := &mockDB{
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			return fmt.Errorf("inTx error")
		},
	}
	db := NewDatabaseWithImpl(mock, "mock")
	err := db.InTransaction(context.Background(), func(tx *sql.Tx) error {
		return nil
	})
	assert.EqualError(t, err, "inTx error")
}

func TestDatabase_Ping_WithMockFn(t *testing.T) {
	mock := &mockDB{pingErr: fmt.Errorf("ping error")}
	db := NewDatabaseWithImpl(mock, "mock")
	err := db.Ping(context.Background())
	assert.EqualError(t, err, "ping error")
}

func TestDatabase_Close_WithMockFn(t *testing.T) {
	closeErr := fmt.Errorf("close error")
	mock := &mockDB{}

	called := false
	mock2 := &mockDBCloser{mockDB: mock, closeErr: closeErr, closeCalled: &called}
	db2 := NewDatabaseWithImpl(mock2, "mock")
	err := db2.Close()
	assert.EqualError(t, err, "close error")
	assert.True(t, called)
}

type mockDBCloser struct {
	*mockDB
	closeErr    error
	closeCalled *bool
}

func (m *mockDBCloser) Close() error {
	*m.closeCalled = true
	return m.closeErr
}

func TestMemoryChallengeStore_ConcurrentSaveAndGet(t *testing.T) {
	store := NewMemoryChallengeStore()
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ch := &WalletChallenge{
				ID:            fmt.Sprintf("ch-%d", idx),
				WalletAddress: fmt.Sprintf("0xwallet%d", idx),
				Nonce:         fmt.Sprintf("nonce-%d", idx),
				ExpiresAt:     time.Now().Add(5 * time.Minute),
			}
			err := store.SaveChallenge(context.Background(), ch)
			assert.NoError(t, err)

			got, err := store.GetChallenge(context.Background(), ch.ID)
			assert.NoError(t, err)
			assert.Equal(t, ch.WalletAddress, got.WalletAddress)
		}(i)
	}
	wg.Wait()
}

func TestMemoryChallengeStore_MarkUsed_Concurrent(t *testing.T) {
	store := NewMemoryChallengeStore()
	defer store.Close()

	ch := &WalletChallenge{
		ID:            "concurrent-ch",
		WalletAddress: "0xwallet",
		Nonce:         "nonce",
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}
	err := store.SaveChallenge(context.Background(), ch)
	require.NoError(t, err)

	var wg sync.WaitGroup
	successCount := int64(0)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.MarkChallengeUsed(context.Background(), "concurrent-ch", time.Now())
			if err == nil {
				successCount++
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(1), successCount)
}

func TestMemoryTokenBlacklist_IsRevoked_ExpiredEntryCleanup(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	err := bl.Revoke(context.Background(), "expired-jti-cleanup", time.Now().Add(-time.Second))
	require.NoError(t, err)

	revoked := bl.IsRevoked(context.Background(), "expired-jti-cleanup")
	assert.False(t, revoked)
}

func TestRedisTokenBlacklist_IsRevoked_FailClosed_ExpiredLocal(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rbl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)

	rbl.FailClosed = true

	err = rbl.Revoke(context.Background(), "expired-local-fc", time.Now().Add(-time.Second))
	require.NoError(t, err)

	mr.Close()

	revoked := rbl.IsRevoked(context.Background(), "expired-local-fc")
	assert.True(t, revoked)
}

func TestRedisTokenBlacklist_Revoke_AlreadyExpired_NoRedisWrite(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rbl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)
	defer rbl.Close()

	err = rbl.Revoke(context.Background(), "already-expired-nw", time.Now().Add(-time.Hour))
	assert.NoError(t, err)

	exists := mr.Exists(blacklistKeyPrefix + "already-expired-nw")
	assert.False(t, exists)
}

func TestAuditLogger_LogAfterClose(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	al.Start()
	al.Close()

	al.Log(context.Background(), "action", "actor", "resource", "id", true, "", "")
}

func TestAuditLogger_Persist_NilDB(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	al.Start()
	defer al.Close()

	al.Log(context.Background(), "action", "actor", "resource", "id", true, "", "")
	time.Sleep(50 * time.Millisecond)
}

func TestAuditLogger_DrainOnCancel(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	al.Start()

	for i := 0; i < 5; i++ {
		al.Log(context.Background(), "action", "actor", "resource", "id", true, "", "")
	}

	al.Close()
}

func TestMinIOStorage_DetectContentType_Table(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"mp4", "video.mp4", "video/mp4"},
		{"webm", "clip.webm", "video/webm"},
		{"mkv", "movie.mkv", "video/x-matroska"},
		{"avi", "film.avi", "video/x-msvideo"},
		{"mov", "clip.mov", "video/quicktime"},
		{"flv", "stream.flv", "video/x-flv"},
		{"wmv", "video.wmv", "video/x-ms-wmv"},
		{"m4v", "video.m4v", "video/mp4"},
		{"3gp", "mobile.3gp", "video/3gpp"},
		{"ogv", "video.ogv", "video/ogg"},
		{"ts", "segment.ts", "video/mp2t"},
		{"m3u8", "playlist.m3u8", "application/vnd.apple.mpegurl"},
		{"mpd", "manifest.mpd", "application/dash+xml"},
		{"m4s", "init.m4s", "video/iso.segment"},
		{"unknown", "file.xyz", "application/octet-stream"},
		{"no_ext", "file", "application/octet-stream"},
		{"uppercase", "VIDEO.MP4", "video/mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectContentTypeByExt(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMinIOStorage_Close_ReturnsNil(t *testing.T) {
	ms := &MinIOStorage{}
	assert.NoError(t, ms.Close())
}

func TestWalletChallenge_Fields_Complete(t *testing.T) {
	now := time.Now()
	ch := &WalletChallenge{
		ID:            "test-id",
		WalletAddress: "0xabc",
		ChainID:       1,
		SigningType:   "personal_sign",
		Nonce:         "nonce-123",
		Message:       "msg",
		IssuedAt:      now,
		ExpiresAt:     now,
		UsedAt:        now,
	}
	assert.Equal(t, "test-id", ch.ID)
	assert.Equal(t, "0xabc", ch.WalletAddress)
	assert.Equal(t, int64(1), ch.ChainID)
	assert.Equal(t, "personal_sign", ch.SigningType)
	assert.Equal(t, "nonce-123", ch.Nonce)
	assert.Equal(t, now, ch.ExpiresAt)
	assert.Equal(t, now, ch.UsedAt)
}

func TestCacheStorage_EvictLRU_SmallMaxSize_One(t *testing.T) {
	cs := NewCacheStorage(1)
	defer cs.Close()

	err := cs.Set("a", "val-a")
	require.NoError(t, err)
	err = cs.Set("b", "val-b")
	require.NoError(t, err)

	_, err = cs.Get("a")
	assert.Error(t, err)

	val, err := cs.Get("b")
	assert.NoError(t, err)
	assert.Equal(t, "val-b", val)
}

func TestCacheStorage_SetWithExpiration_NegativeTTL(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.SetWithExpiration("neg", "value", -1*time.Second)
	require.NoError(t, err)

	val, err := cs.Get("neg")
	assert.NoError(t, err)
	assert.Equal(t, "value", val)
}

func TestCacheStorage_Exists_ExpiredKey(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.SetWithExpiration("exp", "value", 1*time.Millisecond)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	assert.False(t, cs.Exists("exp"))
}

func TestCacheAdapter_SetWithExpiration_Wrap(t *testing.T) {
	cs := NewCacheStorage(100)
	adapter := NewCacheAdapter(cs)
	defer adapter.Close()

	err := adapter.SetWithExpiration(context.Background(), "key", "value", 5*time.Minute)
	assert.NoError(t, err)

	val, err := adapter.Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Equal(t, "value", val)
}

func TestCacheAdapter_Delete_Wrap(t *testing.T) {
	cs := NewCacheStorage(100)
	adapter := NewCacheAdapter(cs)
	defer adapter.Close()

	err := adapter.Set(context.Background(), "key", "value")
	require.NoError(t, err)

	err = adapter.Delete(context.Background(), "key")
	assert.NoError(t, err)

	exists, err := adapter.Exists(context.Background(), "key")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestMigrator_SetTableName_InvalidChars(t *testing.T) {
	m := &Migrator{table: "default"}
	tests := []struct {
		name  string
		input string
	}{
		{"space", "my table"},
		{"dash", "my-table"},
		{"dot", "my.table"},
		{"semicolon", "my;table"},
		{"special", "my$table"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.SetTableName(tt.input)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid table name")
		})
	}
}

func TestMigrator_SetTableName_ValidChars(t *testing.T) {
	m := &Migrator{table: "default"}
	tests := []struct {
		name  string
		input string
	}{
		{"simple", "mytable"},
		{"underscore", "my_table"},
		{"numbers", "table123"},
		{"mixed", "tbl_2024_v2"},
		{"uppercase", "MYTABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.SetTableName(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.input, m.table)
		})
	}
}

func TestMigrator_SetTableName_EmptyString(t *testing.T) {
	m := &Migrator{table: "default"}
	err := m.SetTableName("")
	assert.NoError(t, err)
}

func TestPostgresDB_Constants_Values(t *testing.T) {
	assert.Equal(t, 3, defaultMaxRetries)
	assert.Equal(t, time.Second, defaultRetryBackoff)
	assert.Equal(t, 25, defaultMaxOpenConns)
	assert.Equal(t, 12, defaultMaxIdleConns)
	assert.Equal(t, 15*time.Minute, defaultConnMaxLifetime)
	assert.Equal(t, 5*time.Minute, defaultConnMaxIdleTime)
}

func TestRedisCache_Constants_Values(t *testing.T) {
	assert.Equal(t, 3, redisMaxRetries)
	assert.Equal(t, time.Second, redisRetryBackoff)
}

func TestRedisChallengeStore_GetChallenge_NilClient(t *testing.T) {
	store := &RedisChallengeStore{client: nil, ttl: 5 * time.Minute}
	assert.Panics(t, func() {
		_, _ = store.GetChallenge(context.Background(), "any-id")
	})
}

func TestRedisChallengeStore_MarkChallengeUsed_NilClient(t *testing.T) {
	store := &RedisChallengeStore{client: nil, ttl: 5 * time.Minute}
	assert.Panics(t, func() {
		_ = store.MarkChallengeUsed(context.Background(), "any-id", time.Now())
	})
}

func TestRedisChallengeStore_MarkChallengeUsed_LuaScriptResult(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisChallengeStoreWithClient(client, 5*time.Minute)
	defer store.Close()
	defer mr.Close()

	ch := &WalletChallenge{
		ID:            "lua-test",
		WalletAddress: "0xabc",
		Nonce:         "nonce",
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}
	err = store.SaveChallenge(context.Background(), ch)
	require.NoError(t, err)

	err = store.MarkChallengeUsed(context.Background(), "lua-test", time.Now())
	assert.NoError(t, err)
}

func TestRedisTokenBlacklist_Revoke_RedisFails_LocalCaches(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rbl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)

	err = rbl.Revoke(context.Background(), "local-cache-test", time.Now().Add(time.Hour))
	require.NoError(t, err)

	mr.Close()

	rbl.FailClosed = false
	revoked := rbl.IsRevoked(context.Background(), "local-cache-test")
	assert.True(t, revoked)
}

func TestRedisTokenBlacklist_IsRevoked_FailOpen_NoLocalEntry(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rbl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)

	rbl.FailClosed = false

	mr.Close()

	revoked := rbl.IsRevoked(context.Background(), "never-revoked-fo")
	assert.False(t, revoked)
}

func TestRedisTokenBlacklist_IsRevoked_FailClosed_NoLocalEntryV2(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rbl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)

	rbl.FailClosed = true

	mr.Close()

	revoked := rbl.IsRevoked(context.Background(), "never-revoked-fc")
	assert.True(t, revoked)
}

func TestAuditLogger_BufferFull_DropsEntry(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())

	for i := 0; i < auditBufferSize+50; i++ {
		al.Log(context.Background(), "action", "actor", "resource", "id", true, "", "")
	}

	al.Start()
	al.Close()
}

func TestCacheStorage_EvictLRU_MultipleEvictions(t *testing.T) {
	cs := NewCacheStorage(10)
	defer cs.Close()

	for i := 0; i < 15; i++ {
		err := cs.Set(fmt.Sprintf("key-%d", i), fmt.Sprintf("val-%d", i))
		require.NoError(t, err)
	}

	assert.Equal(t, 10, cs.Size())
}

func TestCacheStorage_Clear_AfterClose(t *testing.T) {
	cs := NewCacheStorage(10)
	err := cs.Set("key", "value")
	require.NoError(t, err)
	cs.Close()

	assert.Equal(t, 0, cs.Size())
}

func TestNATSQueue_Constants(t *testing.T) {
	assert.Equal(t, "TRANSCODING", jsStreamName)
	assert.Equal(t, "streamgate.transcoding.tasks", jsStreamSubject)
	assert.Equal(t, "transcoding-worker", jsConsumerName)
	assert.Equal(t, "TRANSCODING_DLQ", jsDLQStreamName)
	assert.Equal(t, "streamgate.transcoding.dlq", jsDLQStreamSubject)
	assert.Equal(t, 5, jsMaxDeliver)
	assert.Equal(t, 30*time.Minute, msgStaleTimeout)
	assert.Equal(t, 2*time.Hour, statusStaleTimeout)
	assert.Equal(t, 5*time.Minute, cleanupInterval)
}
