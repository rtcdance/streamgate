package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewPostgresAuditLogger(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	assert.NotNil(t, al)
}

func TestPostgresAuditLogger_Log(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	assert.NotPanics(t, func() {
		al.Log(context.Background(), "content.create", "owner1", "content", "c1", true, "", "test")
	})
}

func TestPostgresAuditLogger_StartAndClose(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	al.Start()

	al.Log(context.Background(), "test.action", "actor1", "resource", "r1", true, "", "detail")

	err := al.Close()
	assert.NoError(t, err)
}

func TestPostgresAuditLogger_LogBufferFull(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	assert.NotPanics(t, func() {
		for i := 0; i < 2000; i++ {
			al.Log(context.Background(), "test.action", "actor", "resource", "r", true, "", "")
		}
	})
	al.Close()
}

func TestPostgresAuditLogger_Persist_NilDB(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	assert.NotPanics(t, func() {
		al.persist(auditEntry{
			action:     "test",
			actor:      "actor1",
			resource:   "content",
			resourceID: "c1",
			success:    true,
			timestamp:  time.Now(),
		})
	})
}

func TestPostgresAuditLogger_Persist_WithDB(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	pdb := NewPostgresDBFromDB(db)
	al := NewPostgresAuditLogger(pdb, zap.NewNop())

	al.persist(auditEntry{
		action:     "test.action",
		actor:      "actor1",
		resource:   "content",
		resourceID: "c1",
		success:    true,
		errMsg:     "",
		details:    "test details",
		timestamp:  time.Now(),
	})
}

func TestPostgresAuditLogger_Drain(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	al.Log(context.Background(), "test.action", "actor1", "resource", "r1", true, "", "")
	al.drain()
}

func TestPostgresAuditLogger_Drain_Empty(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	al.drain()
}

func TestPostgresAuditLogger_Log_Failure(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	assert.NotPanics(t, func() {
		al.Log(context.Background(), "auth.fail", "attacker", "auth", "a1", false, "invalid credentials", "ip=1.2.3.4")
	})
}

func TestPostgresAuditLogger_MultipleLogsAndClose(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	al.Start()

	for i := 0; i < 100; i++ {
		al.Log(context.Background(), "test.action", "actor", "resource", "r", true, "", "")
	}

	err := al.Close()
	assert.NoError(t, err)
}

func TestPostgresAuditLogger_Close_WithoutStart(t *testing.T) {
	al := NewPostgresAuditLogger(nil, zap.NewNop())
	err := al.Close()
	assert.NoError(t, err)
}

func TestPostgresAuditLogger_ImplementsAuditLogger(t *testing.T) {
	var _ AuditLogger = NewPostgresAuditLogger(nil, zap.NewNop())
}

func TestAuditEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := auditEntry{
		action:     "content.create",
		actor:      "user1",
		resource:   "content",
		resourceID: "c1",
		success:    true,
		errMsg:     "",
		details:    "created new content",
		timestamp:  now,
	}
	assert.Equal(t, "content.create", entry.action)
	assert.Equal(t, "user1", entry.actor)
	assert.Equal(t, "content", entry.resource)
	assert.Equal(t, "c1", entry.resourceID)
	assert.True(t, entry.success)
	assert.Equal(t, now, entry.timestamp)
}

func TestRevocationEntry_IsExpired(t *testing.T) {
	e := &revocationEntry{expiresAt: time.Now().Add(-time.Hour)}
	assert.True(t, e.isExpired())

	e = &revocationEntry{expiresAt: time.Now().Add(time.Hour)}
	assert.False(t, e.isExpired())
}

func TestPostgresAuditLogger_Constants(t *testing.T) {
	assert.Equal(t, 1024, auditBufferSize)
	assert.NotEmpty(t, auditInsertQuery)
}
