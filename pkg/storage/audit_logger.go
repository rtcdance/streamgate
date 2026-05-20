package storage

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	auditBufferSize  = 1024
	auditInsertQuery = `INSERT INTO audit_logs (action, actor, resource, resource_id, success, error_msg, details, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
)

type auditEntry struct {
	action     string
	actor      string
	resource   string
	resourceID string
	success    bool
	errMsg     string
	details    string
	timestamp  time.Time
}

type PostgresAuditLogger struct {
	db     *PostgresDB
	logger *zap.Logger
	ch     chan auditEntry
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func NewPostgresAuditLogger(db *PostgresDB, logger *zap.Logger) *PostgresAuditLogger {
	ctx, cancel := context.WithCancel(context.Background())
	return &PostgresAuditLogger{
		db:     db,
		logger: logger,
		ch:     make(chan auditEntry, auditBufferSize),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (al *PostgresAuditLogger) Start() {
	al.wg.Add(1)
	go al.worker()
}

func (al *PostgresAuditLogger) Log(ctx context.Context, action, actor, resource, resourceID string, success bool, errMsg, details string) {
	entry := auditEntry{
		action:     action,
		actor:      actor,
		resource:   resource,
		resourceID: resourceID,
		success:    success,
		errMsg:     errMsg,
		details:    details,
		timestamp:  time.Now(),
	}

	select {
	case al.ch <- entry:
	default:
		al.logger.Warn("audit log buffer full, dropping entry",
			zap.String("action", action),
			zap.String("actor", actor))
	}
}

func (al *PostgresAuditLogger) worker() {
	defer al.wg.Done()

	for {
		select {
		case <-al.ctx.Done():
			al.drain()
			return
		case entry := <-al.ch:
			al.persist(entry)
		}
	}
}

func (al *PostgresAuditLogger) persist(entry auditEntry) {
	if al.db == nil || al.db.db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := al.db.Exec(ctx, auditInsertQuery,
		entry.action,
		entry.actor,
		entry.resource,
		entry.resourceID,
		entry.success,
		entry.errMsg,
		entry.details,
		entry.timestamp,
	)
	if err != nil {
		al.logger.Warn("failed to persist audit log",
			zap.String("action", entry.action),
			zap.Error(err))
	}
}

func (al *PostgresAuditLogger) drain() {
	for {
		select {
		case entry := <-al.ch:
			al.persist(entry)
		default:
			return
		}
	}
}

func (al *PostgresAuditLogger) Close() error {
	al.cancel()
	al.wg.Wait()
	return nil
}
