package transcoding

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The durability acceptance tests need lease/expiry semantics evaluated
// against a controllable DB clock (the production SQL judges them with NOW()).
// fakeTaskDB is a registered database/sql driver that interprets exactly the
// statements TranscodingService issues against an in-memory task table with an
// injectable clock, and gives real transaction semantics (rollback restores
// the snapshot taken at Begin) so same-transaction atomicity is observable.

const durabilityDriverName = "streamgate_durability"

var globalFakeDB = &fakeTaskDB{contents: map[string]string{}}

func init() { sql.Register(durabilityDriverName, globalFakeDB) }

type fakeTaskDB struct {
	mu       sync.Mutex
	now      time.Time
	tasks    map[string]*fakeTaskRow
	outbox   map[string]*fakeOutboxRow
	contents map[string]string
	// failNextOutboxInsert makes the next INSERT INTO outbox fail, so tests
	// can prove the terminal-failure transaction rolls back atomically.
	failNextOutboxInsert bool
}

type fakeTaskRow struct {
	id             string
	contentID      string
	profile        string
	status         string
	progress       int
	inputURL       string
	outputURL      string
	errMsg         string
	lastError      string
	priority       int
	ownerWallet    string
	metadata       []byte
	leaseOwner     string
	leaseExpiresAt time.Time // zero = NULL
	generation     int
	attempt        int
	nextAttemptAt  time.Time // zero = NULL
	startedAt      time.Time
	completedAt    time.Time
}

func (r *fakeTaskRow) clone() *fakeTaskRow {
	c := *r
	return &c
}

type fakeOutboxRow struct {
	id          string
	kind        string
	payload     []byte
	status      string
	attempts    int
	lastError   string
	createdAt   time.Time
	deliveredAt time.Time
}

func (r *fakeOutboxRow) clone() *fakeOutboxRow {
	c := *r
	c.payload = append([]byte(nil), r.payload...)
	return &c
}

// reset installs a fresh state with the given DB clock. Tests call this in
// setup; sequential tests share the registered driver via the global pointer.
func (d *fakeTaskDB) reset(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.now = now
	d.tasks = make(map[string]*fakeTaskRow)
	d.outbox = make(map[string]*fakeOutboxRow)
	d.contents = make(map[string]string)
	d.failNextOutboxInsert = false
}

func (d *fakeTaskDB) advance(dur time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.now = d.now.Add(dur)
}

func (d *fakeTaskDB) dbTime() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.now
}

func (d *fakeTaskDB) task(id string) *fakeTaskRow {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.tasks[id]; ok {
		return r.clone()
	}
	return nil
}

func (d *fakeTaskDB) outboxRow(kind, id string) *fakeOutboxRow {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.outbox[kind+":"+id]; ok {
		return r.clone()
	}
	return nil
}

func (d *fakeTaskDB) Open(string) (driver.Conn, error) { return &fakeConn{d: d}, nil }

type fakeConn struct{ d *fakeTaskDB }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{d: c.d, query: query}, nil
}
func (c *fakeConn) Close() error { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	c.d.mu.Lock()
	snapTasks := make(map[string]*fakeTaskRow, len(c.d.tasks))
	for k, v := range c.d.tasks {
		snapTasks[k] = v.clone()
	}
	snapOutbox := make(map[string]*fakeOutboxRow, len(c.d.outbox))
	for k, v := range c.d.outbox {
		snapOutbox[k] = v.clone()
	}
	c.d.mu.Unlock()
	return &fakeTx{d: c.d, snapTasks: snapTasks, snapOutbox: snapOutbox}, nil
}

type fakeTx struct {
	d          *fakeTaskDB
	snapTasks  map[string]*fakeTaskRow
	snapOutbox map[string]*fakeOutboxRow
}

func (t *fakeTx) Commit() error { return nil }

func (t *fakeTx) Rollback() error {
	t.d.mu.Lock()
	defer t.d.mu.Unlock()
	t.d.tasks = t.snapTasks
	t.d.outbox = t.snapOutbox
	return nil
}

type fakeStmt struct {
	d     *fakeTaskDB
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }

func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	n, err := s.d.execute(s.query, args)
	return fakeResult{rows: n}, err
}

func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.d.query(s.query, args)
}

type fakeResult struct{ rows int64 }

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.rows, nil }

type fakeRows struct {
	cols []string
	data [][]driver.Value
	idx  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.idx])
	r.idx++
	return nil
}

func sq(q string) string { return strings.Join(strings.Fields(q), " ") }

func argStr(args []driver.Value, i int) string {
	if i >= len(args) || args[i] == nil {
		return ""
	}
	return args[i].(string)
}

func argF64(args []driver.Value, i int) float64 {
	if i >= len(args) || args[i] == nil {
		return 0
	}
	return args[i].(float64)
}

func argI64(args []driver.Value, i int) int64 {
	if i >= len(args) || args[i] == nil {
		return 0
	}
	return args[i].(int64)
}

// execute interprets the write statements the transcoding service issues.
func (d *fakeTaskDB) execute(query string, args []driver.Value) (int64, error) {
	q := sq(query)
	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case strings.HasPrefix(q, "INSERT INTO transcoding_tasks"):
		id := argStr(args, 0)
		if _, exists := d.tasks[id]; exists {
			return 0, fmt.Errorf("duplicate key value violates unique constraint")
		}
		created := time.Time{}
		if v, ok := args[9].(time.Time); ok {
			created = v
		}
		var meta []byte
		if args[10] != nil {
			meta = append([]byte(nil), args[10].([]byte)...)
		}
		d.tasks[id] = &fakeTaskRow{
			id: id, contentID: argStr(args, 1), profile: argStr(args, 2),
			status: argStr(args, 3), progress: int(argI64(args, 4)),
			inputURL: argStr(args, 5), outputURL: argStr(args, 6),
			errMsg: argStr(args, 7), priority: int(argI64(args, 8)),
			metadata: meta, ownerWallet: argStr(args, 11),
		}
		_ = created
		return 1, nil

	case strings.HasPrefix(q, "INSERT INTO outbox"):
		if d.failNextOutboxInsert {
			d.failNextOutboxInsert = false
			return 0, errors.New("injected outbox insert failure")
		}
		id, kind := argStr(args, 0), argStr(args, 1)
		key := kind + ":" + id
		if _, exists := d.outbox[key]; exists {
			// ON CONFLICT (kind, id) DO NOTHING
			return 0, nil
		}
		payload := append([]byte(nil), args[2].([]byte)...)
		d.outbox[key] = &fakeOutboxRow{id: id, kind: kind, payload: payload, status: "pending", createdAt: d.now}
		return 1, nil

	case strings.Contains(q, "UPDATE outbox SET status = 'delivered'"):
		key := argStr(args, 1) + ":" + argStr(args, 0)
		row, ok := d.outbox[key]
		if !ok || row.status != "pending" {
			return 0, nil
		}
		row.status = "delivered"
		row.deliveredAt = d.now
		row.lastError = ""
		return 1, nil

	case strings.HasPrefix(q, "UPDATE outbox SET attempts"):
		key := argStr(args, 1) + ":" + argStr(args, 0)
		if row, ok := d.outbox[key]; ok {
			row.attempts++
			row.lastError = argStr(args, 2)
			return 1, nil
		}
		return 0, nil

	case strings.Contains(q, "RETURNING generation, attempt"):
		// Handled in query(): UPDATE ... RETURNING goes through Query in
		// database/sql, but keep a defensive Exec path.
		return d.claimLocked(args)

	case strings.Contains(q, "SET lease_expires_at = NOW()"):
		// renewLease: WHERE id=$1 AND lease_owner=$2 AND generation=$4 AND status='processing'
		row, ok := d.tasks[argStr(args, 0)]
		if !ok || row.status != "processing" || row.leaseOwner != argStr(args, 1) || row.generation != int(argI64(args, 3)) {
			return 0, nil
		}
		row.leaseExpiresAt = d.now.Add(time.Duration(argF64(args, 2) * float64(time.Second)))
		return 1, nil

	case strings.Contains(q, "SET status = 'pending', last_error"):
		// releaseForRetry: WHERE id=$1 AND status='processing' AND lease_owner=$2 AND generation=$5
		row, ok := d.tasks[argStr(args, 0)]
		if !ok || row.status != "processing" || row.leaseOwner != argStr(args, 1) || row.generation != int(argI64(args, 4)) {
			return 0, nil
		}
		row.status = "pending"
		row.lastError = argStr(args, 2)
		row.nextAttemptAt = d.now.Add(time.Duration(argF64(args, 3) * float64(time.Second)))
		row.leaseOwner = ""
		row.leaseExpiresAt = time.Time{}
		return 1, nil

	case strings.Contains(q, "SET status = 'failed'"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok || (row.status != "pending" && row.status != "processing") {
			return 0, nil
		}
		if len(args) >= 4 && (row.leaseOwner != argStr(args, 2) || row.generation != int(argI64(args, 3))) {
			return 0, nil
		}
		row.status = "failed"
		row.errMsg = argStr(args, 1)
		row.lastError = argStr(args, 1)
		row.completedAt = d.now
		return 1, nil

	case strings.Contains(q, "SET status = 'pending', lease_owner = '', lease_expires_at = NULL"):
		// recoverStuckTasks: expired leases judged by DB time.
		var n int64
		for _, row := range d.tasks {
			if row.status == "processing" && !row.leaseExpiresAt.IsZero() && row.leaseExpiresAt.Before(d.now) {
				row.status = "pending"
				row.leaseOwner = ""
				row.leaseExpiresAt = time.Time{}
				n++
			}
		}
		return n, nil

	case strings.Contains(q, "SET status = 'completed'"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok || row.status != "processing" {
			return 0, nil
		}
		// args: id, outputURL, completedAt, [metadata], [owner, generation]
		if len(args) >= 5 && (row.leaseOwner != argStr(args, len(args)-2) || row.generation != int(argI64(args, len(args)-1))) {
			return 0, nil
		}
		row.status = "completed"
		row.progress = 100
		row.outputURL = argStr(args, 1)
		if v, ok := args[2].(time.Time); ok {
			row.completedAt = v
		}
		if len(args) >= 4 && args[3] != nil {
			if _, isMeta := args[3].([]byte); isMeta && len(args) == 4 {
				row.metadata = append([]byte(nil), args[3].([]byte)...)
			}
		}
		return 1, nil

	case strings.Contains(q, "SET progress = $2, metadata = $3"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok {
			return 0, nil
		}
		if len(args) >= 5 && (row.leaseOwner != argStr(args, 3) || row.generation != int(argI64(args, 4))) {
			return 0, nil
		}
		row.progress = int(argI64(args, 1))
		if args[2] != nil {
			row.metadata = append([]byte(nil), args[2].([]byte)...)
		}
		return 1, nil

	case strings.Contains(q, "SET progress = $2 WHERE id"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok {
			return 0, nil
		}
		if len(args) >= 4 && (row.leaseOwner != argStr(args, 2) || row.generation != int(argI64(args, 3))) {
			return 0, nil
		}
		row.progress = int(argI64(args, 1))
		return 1, nil

	case strings.Contains(q, "SET metadata = $2 WHERE id"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok {
			return 0, nil
		}
		if len(args) >= 4 && (row.leaseOwner != argStr(args, 2) || row.generation != int(argI64(args, 3))) {
			return 0, nil
		}
		if args[1] != nil {
			row.metadata = append([]byte(nil), args[1].([]byte)...)
		}
		return 1, nil

	case strings.Contains(q, "SET status = $2, error = $3, completed_at"):
		// legacy FailTask (no lease path)
		row, ok := d.tasks[argStr(args, 0)]
		if !ok || (row.status != "pending" && row.status != "processing") {
			return 0, nil
		}
		row.status = argStr(args, 1)
		row.errMsg = argStr(args, 2)
		if v, ok := args[3].(time.Time); ok {
			row.completedAt = v
		}
		return 1, nil

	case strings.Contains(q, "SET status = $2, started_at"):
		// legacy StartTask CAS
		row, ok := d.tasks[argStr(args, 0)]
		if !ok || row.status != "pending" {
			return 0, nil
		}
		row.status = argStr(args, 1)
		return 1, nil

	case strings.HasPrefix(q, "UPDATE contents"):
		return 1, nil
	}
	return 0, fmt.Errorf("fakeTaskDB: unhandled exec: %s", q)
}

func (d *fakeTaskDB) claimLocked(args []driver.Value) (int64, error) {
	row, ok := d.tasks[argStr(args, 0)]
	maxAttempt := int(argI64(args, 3))
	claimable := ok && row.attempt < maxAttempt &&
		(row.status == "pending" ||
			(row.status == "processing" && !row.leaseExpiresAt.IsZero() && row.leaseExpiresAt.Before(d.now)))
	if !claimable {
		return 0, nil
	}
	row.status = "processing"
	row.leaseOwner = argStr(args, 1)
	row.leaseExpiresAt = d.now.Add(time.Duration(argF64(args, 2) * float64(time.Second)))
	row.generation++
	row.attempt++
	row.nextAttemptAt = time.Time{}
	row.startedAt = d.now
	return 1, nil
}

// query interprets the read statements the transcoding service issues.
func (d *fakeTaskDB) query(query string, args []driver.Value) (driver.Rows, error) {
	q := sq(query)
	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case strings.Contains(q, "SELECT status, attempt,"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok {
			return &fakeRows{cols: []string{"status", "attempt", "lease_valid"}}, nil
		}
		leaseValid := row.status == "processing" && !row.leaseExpiresAt.IsZero() && row.leaseExpiresAt.After(d.now)
		return &fakeRows{
			cols: []string{"status", "attempt", "lease_valid"},
			data: [][]driver.Value{{row.status, int64(row.attempt), leaseValid}},
		}, nil

	case strings.Contains(q, "RETURNING generation, attempt"):
		// UPDATE ... RETURNING goes through Query in database/sql.
		n, err := d.claimLocked(args)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return &fakeRows{cols: []string{"generation", "attempt"}}, nil
		}
		row := d.tasks[argStr(args, 0)]
		return &fakeRows{
			cols: []string{"generation", "attempt"},
			data: [][]driver.Value{{int64(row.generation), int64(row.attempt)}},
		}, nil

	case strings.HasPrefix(q, "SELECT id, kind, payload FROM outbox"):
		var data [][]driver.Value
		for _, key := range sortedKeys(d.outbox) {
			row := d.outbox[key]
			if row.status == "pending" {
				data = append(data, []driver.Value{row.id, row.kind, row.payload})
			}
			if len(data) >= 25 {
				break
			}
		}
		return &fakeRows{cols: []string{"id", "kind", "payload"}, data: data}, nil

	case strings.Contains(q, "SELECT payload, status FROM outbox"):
		row, ok := d.outbox[argStr(args, 1)+":"+argStr(args, 0)]
		if !ok {
			return &fakeRows{cols: []string{"payload", "status"}}, nil
		}
		return &fakeRows{
			cols: []string{"payload", "status"},
			data: [][]driver.Value{{row.payload, row.status}},
		}, nil

	case strings.Contains(q, "SELECT content_id FROM transcoding_tasks WHERE id"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok {
			return &fakeRows{cols: []string{"content_id"}}, nil
		}
		return &fakeRows{cols: []string{"content_id"}, data: [][]driver.Value{{row.contentID}}}, nil

	case strings.Contains(q, "SELECT COUNT(*) FROM transcoding_tasks WHERE content_id"):
		var n int64
		for _, row := range d.tasks {
			if row.contentID == argStr(args, 0) && (row.status == "pending" || row.status == "processing") {
				n++
			}
		}
		return &fakeRows{cols: []string{"count"}, data: [][]driver.Value{{n}}}, nil

	case strings.Contains(q, "SELECT url FROM contents WHERE id"):
		url, ok := d.contents[argStr(args, 0)]
		if !ok {
			return &fakeRows{cols: []string{"url"}}, nil
		}
		return &fakeRows{cols: []string{"url"}, data: [][]driver.Value{{url}}}, nil

	case strings.Contains(q, "SELECT id, content_id, profile, status, progress"):
		row, ok := d.tasks[argStr(args, 0)]
		if !ok {
			return &fakeRows{cols: []string{"id"}}, nil
		}
		var started, completed driver.Value
		if !row.startedAt.IsZero() {
			started = row.startedAt
		}
		if !row.completedAt.IsZero() {
			completed = row.completedAt
		}
		meta := row.metadata
		if meta == nil {
			meta = []byte("{}")
		}
		return &fakeRows{
			cols: []string{"id", "content_id", "profile", "status", "progress", "input_url", "output_url", "error", "priority", "created_at", "started_at", "completed_at", "metadata", "owner_wallet"},
			data: [][]driver.Value{{
				row.id, row.contentID, row.profile, row.status, int64(row.progress),
				row.inputURL, row.outputURL, row.errMsg, int64(row.priority),
				d.now, started, completed, meta, row.ownerWallet,
			}},
		}, nil
	}
	return nil, fmt.Errorf("fakeTaskDB: unhandled query: %s", q)
}

func sortedKeys(m map[string]*fakeOutboxRow) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// stable enough for tests: sort by insertion-independent key order
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// fakeStorageDB adapts a *sql.DB (backed by the fake driver) to storage.DB so
// the service's non-tx statements and InTransaction both hit the fake table.
type fakeStorageDB struct{ sqlDB *sql.DB }

func (f *fakeStorageDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	return f.sqlDB.QueryContext(ctx, query, args...)
}
func (f *fakeStorageDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	return stg.NewCancelRow(f.sqlDB.QueryRowContext(ctx, query, args...), func() {})
}
func (f *fakeStorageDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return f.sqlDB.ExecContext(ctx, query, args...)
}
func (f *fakeStorageDB) Begin(ctx context.Context) (*sql.Tx, error) { return f.sqlDB.BeginTx(ctx, nil) }
func (f *fakeStorageDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := f.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
func (f *fakeStorageDB) Ping(context.Context) error { return nil }
func (f *fakeStorageDB) Close() error               { return f.sqlDB.Close() }

func newDurabilityFixture(t *testing.T, queue TranscodingQueue, xcoder VideoTranscoder) (*TranscodingService, *fakeTaskDB) {
	t.Helper()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	globalFakeDB.reset(now)
	sqlDB, err := sql.Open(durabilityDriverName, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	svc := NewTranscodingService(&fakeStorageDB{sqlDB: sqlDB}, queue,
		WithTranscoder(xcoder),
		WithLogger(zap.NewNop()),
		WithLeaseOwner("worker-A"),
	)
	return svc, globalFakeDB
}

type published struct {
	kind    string
	msgID   string
	payload []byte
}

type nakRecord struct {
	taskID string
	delay  time.Duration
}

// recordingQueue is a scriptable TranscodingQueue: it records publishes, Acks
// and Naks, and can inject publish failures to simulate a missing PubAck.
type recordingQueue struct {
	mu         sync.Mutex
	publishes  []published
	publishErr map[string]error
	acks       []string
	naks       []nakRecord
	inProgress []string
}

func newRecordingQueue() *recordingQueue {
	return &recordingQueue{publishErr: make(map[string]error)}
}

func (q *recordingQueue) Enqueue(task *models.TranscodingTask) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return q.PublishOutbox(models.OutboxKindTask, task.ID, data)
}

func (q *recordingQueue) Dequeue(context.Context) (*models.TranscodingTask, models.DeliveryHandle, error) {
	return nil, nil, errors.New("empty")
}

func (q *recordingQueue) Ack(h models.DeliveryHandle) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acks = append(q.acks, h.TaskID())
	return nil
}

func (q *recordingQueue) Nak(h models.DeliveryHandle, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.naks = append(q.naks, nakRecord{taskID: h.TaskID(), delay: delay})
	return nil
}

func (q *recordingQueue) InProgress(h models.DeliveryHandle) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inProgress = append(q.inProgress, h.TaskID())
	return nil
}

func (q *recordingQueue) PublishOutbox(kind, msgID string, payload []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err, ok := q.publishErr[kind]; ok && err != nil {
		return err
	}
	q.publishes = append(q.publishes, published{kind: kind, msgID: msgID, payload: append([]byte(nil), payload...)})
	return nil
}

func (q *recordingQueue) Depth() (int, error) { return 0, nil }

func (q *recordingQueue) ackCount(taskID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, id := range q.acks {
		if id == taskID {
			n++
		}
	}
	return n
}

func (q *recordingQueue) nakDelays(taskID string) []time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []time.Duration
	for _, r := range q.naks {
		if r.taskID == taskID {
			out = append(out, r.delay)
		}
	}
	return out
}

func (q *recordingQueue) publishCount(kind, msgID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, p := range q.publishes {
		if p.kind == kind && p.msgID == msgID {
			n++
		}
	}
	return n
}

// testDelivery is a per-delivery handle for the recording queue.
type testDelivery struct {
	q      *recordingQueue
	taskID string
}

func (h *testDelivery) TaskID() string { return h.taskID }

func deliverTo(svc *TranscodingService, q *recordingQueue, task *models.TranscodingTask) {
	svc.handleDelivery(context.Background(), task, &testDelivery{q: q, taskID: task.ID}, zap.NewNop())
}

// TestAcceptance_TranscodeRedeliveryAndCrash pins the crash-recovery contract:
// task + enqueue intent commit atomically; the claim takes a DB-time lease and
// bumps attempt only when it wins; a crashed claim is recovered by lease
// expiry (generation and attempt preserved); the redelivered message then
// re-processes without duplicating side effects; a settled task's redelivery
// is Acked with zero side effects.
func TestAcceptance_TranscodeRedeliveryAndCrash(t *testing.T) {
	queue := newRecordingQueue()
	var hookCalls int
	xcoder := &mockVideoTranscoder{progressCalls: []float64{100}}
	svc, db := newDurabilityFixture(t, queue, xcoder)
	svc.RegisterPostTranscodeHook(func(context.Context, string, string, string) { hookCalls++ })

	ctx := context.Background()
	taskID, err := svc.Transcode(ctx, "content-crash", "720p", "/tmp/in.mp4", 5, "0xOwner")
	require.NoError(t, err)

	// 1. Task row and enqueue intent committed together; message published
	// with the taskID as its stable ID; intent marked delivered only after
	// the publish returned.
	require.NotNil(t, db.task(taskID), "task row must exist")
	intent := db.outboxRow(models.OutboxKindTask, taskID)
	require.NotNil(t, intent, "enqueue intent must exist in the same tx as the task")
	assert.Equal(t, "delivered", intent.status)
	assert.Equal(t, 1, queue.publishCount(models.OutboxKindTask, taskID))

	// 2. Claim with lease: generation 1, attempt 1, lease valid for 30s of DB
	// time.
	lease, ok, err := svc.claim(ctx, taskID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1, lease.generation)
	assert.Equal(t, 1, lease.attempt)
	row := db.task(taskID)
	assert.Equal(t, "processing", row.status)
	assert.Equal(t, "worker-A", row.leaseOwner)
	assert.Equal(t, db.dbTime().Add(defaultLeaseTTL).Unix(), row.leaseExpiresAt.Unix())

	// 3. A second claim (another worker, or a duplicate delivery) must not
	// succeed while the lease is live, and must NOT burn an attempt.
	_, ok, err = svc.claim(ctx, taskID)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 1, db.task(taskID).attempt, "failed claim must not increment attempt")

	// 4. Crash: the claimer dies without settling. renewLease from the dead
	// worker reports the loss once the row is recovered.
	held, err := svc.renewLease(ctx, lease)
	require.NoError(t, err)
	assert.True(t, held, "lease is still valid before the crash")

	// 5. Recovery judges expiry by DB time: advance past the lease, the row
	// resets to pending with generation and attempt preserved.
	db.advance(defaultLeaseTTL + time.Second)
	svc.recoverStuckTasks(zap.NewNop())
	row = db.task(taskID)
	require.Equal(t, "pending", row.status)
	assert.Equal(t, 1, row.generation, "recovery preserves generation")
	assert.Equal(t, 1, row.attempt, "recovery preserves attempt")
	assert.Empty(t, row.leaseOwner)

	// Lease renewal from the dead worker now reports the loss.
	held, err = svc.renewLease(ctx, lease)
	require.NoError(t, err)
	assert.False(t, held, "expired lease must not renew")

	// 6. The redelivered message re-claims (generation 2, attempt 2) and
	// re-processes to completion — side effects happen exactly once.
	redelivered := &models.TranscodingTask{ID: taskID, ContentID: "content-crash", Profile: "720p", InputURL: "/tmp/in.mp4"}
	deliverTo(svc, queue, redelivered)

	row = db.task(taskID)
	require.Equal(t, "completed", row.status)
	assert.Equal(t, 2, row.generation)
	assert.Equal(t, 2, row.attempt, "attempt increments only on a successful claim")
	assert.Contains(t, row.outputURL, "streams/content-crash/720p")
	assert.Equal(t, 1, hookCalls, "post-transcode side effects must run exactly once")
	assert.Equal(t, 1, queue.ackCount(taskID))

	// 7. Redelivery of the settled task: Ack immediately, zero side effects.
	dup := &models.TranscodingTask{ID: taskID, ContentID: "content-crash", Profile: "720p", InputURL: "/tmp/in.mp4"}
	deliverTo(svc, queue, dup)
	assert.Equal(t, 2, queue.ackCount(taskID))
	assert.Equal(t, 1, hookCalls, "duplicate delivery must not re-run side effects")
	assert.Equal(t, 2, db.task(taskID).generation, "duplicate delivery must not re-claim")

	// 8. Status GET is answered by Postgres even when the in-memory mirror
	// disagrees: seed a stale "processing" mirror entry.
	svc.storeTask(&models.TranscodingTask{ID: taskID, Status: "processing", Metadata: map[string]interface{}{}})
	got, err := svc.GetTranscodingStatus(ctx, taskID)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status, "status authority is Postgres, never the process-local map")
}

// TestAcceptance_RetryAndDLQRecovery pins the retry/DLQ contract: three
// attempts max with a 2s/4s schedule recorded in next_attempt_at and the Nak
// delay; terminal failure writes status=failed and the DLQ intent in ONE
// transaction; the intent is marked delivered only after the broker ack (a
// publish failure leaves it pending for the pump); redelivery of failed and
// completed tasks never duplicates side effects.
func TestAcceptance_RetryAndDLQRecovery(t *testing.T) {
	queue := newRecordingQueue()
	xcoder := &mockVideoTranscoder{transcodeErr: errors.New("ffmpeg boom")}
	svc, db := newDurabilityFixture(t, queue, xcoder)

	ctx := context.Background()
	taskID, err := svc.Transcode(ctx, "content-retry", "720p", "/tmp/in.mp4", 5, "0xOwner")
	require.NoError(t, err)
	require.Equal(t, "delivered", db.outboxRow(models.OutboxKindTask, taskID).status)

	attemptTask := func() {
		deliverTo(svc, queue, &models.TranscodingTask{ID: taskID, ContentID: "content-retry", Profile: "720p", InputURL: "/tmp/in.mp4"})
	}

	// Attempt 1 fails -> release to pending with last_error and
	// next_attempt_at = DB now + 2s; the SAME message is Nak'd with 2s.
	attemptTask()
	row := db.task(taskID)
	require.Equal(t, "pending", row.status)
	assert.Equal(t, 1, row.attempt)
	assert.Contains(t, row.lastError, "ffmpeg boom")
	assert.Equal(t, db.dbTime().Add(2*time.Second).Unix(), row.nextAttemptAt.Unix())
	assert.Equal(t, []time.Duration{2 * time.Second}, queue.nakDelays(taskID))
	assert.Equal(t, 0, queue.ackCount(taskID))

	// Attempt 2 fails -> next wait is 4s.
	db.advance(2 * time.Second)
	attemptTask()
	row = db.task(taskID)
	require.Equal(t, "pending", row.status)
	assert.Equal(t, 2, row.attempt)
	assert.Equal(t, db.dbTime().Add(4*time.Second).Unix(), row.nextAttemptAt.Unix())
	assert.Equal(t, []time.Duration{2 * time.Second, 4 * time.Second}, queue.nakDelays(taskID))

	// Attempt 3 fails -> terminal: status=failed AND DLQ intent in ONE
	// transaction. Inject an outbox failure first to prove atomicity: the
	// whole terminal tx must roll back, leaving no failed status and no
	// intent behind.
	db.advance(4 * time.Second)
	queue.mu.Lock()
	queue.publishErr[models.OutboxKindDLQ] = errors.New("nats down")
	queue.mu.Unlock()
	db.mu.Lock()
	db.failNextOutboxInsert = true
	db.mu.Unlock()
	attemptTask()
	row = db.task(taskID)
	assert.Equal(t, "processing", row.status, "terminal write must be atomic: rollback leaves no failed status")
	assert.Nil(t, db.outboxRow(models.OutboxKindDLQ, taskID), "no DLQ intent without the failed status")

	// The rolled-back tx left the row processing under the (soon stale)
	// attempt-3 lease. Once that lease expires on the DB clock, the next
	// redelivery goes straight to the terminal path — attempt is already at
	// the cap, so no extra attempt is consumed — and this time the tx
	// commits. The DLQ publish then fails (no PubAck): the intent STAYS
	// pending and the original message is NAK'd, never Acked.
	db.advance(defaultLeaseTTL + time.Second)
	attemptTask()
	row = db.task(taskID)
	require.Equal(t, "failed", row.status)
	assert.Equal(t, 3, row.attempt, "terminal path does not consume an extra attempt")
	intent := db.outboxRow(models.OutboxKindDLQ, taskID)
	require.NotNil(t, intent, "DLQ intent must be written together with the failed status")
	assert.Equal(t, "pending", intent.status)
	assert.Equal(t, "pending", db.outboxRow(models.OutboxKindDLQ, taskID).status, "publish failure leaves a recoverable intent")
	assert.Equal(t, 0, queue.ackCount(taskID), "message must not be Acked before the DLQ PubAck")

	// Publisher recovers: the pump publishes the intent and marks it
	// delivered only after the (simulated) PubAck.
	queue.mu.Lock()
	delete(queue.publishErr, models.OutboxKindDLQ)
	queue.mu.Unlock()
	n, err := svc.pumpOutbox(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "delivered", db.outboxRow(models.OutboxKindDLQ, taskID).status)
	assert.Equal(t, 1, queue.publishCount(models.OutboxKindDLQ, taskID))

	// Redelivery of the failed task: the DLQ intent is already delivered, so
	// the message is Acked with zero side effects and no re-publish.
	acksBefore := queue.ackCount(taskID)
	attemptTask()
	assert.Equal(t, acksBefore+1, queue.ackCount(taskID))
	assert.Equal(t, 1, queue.publishCount(models.OutboxKindDLQ, taskID), "delivered intent must not be republished")
	assert.Equal(t, 3, db.task(taskID).attempt, "no extra claim for a failed task")

	// Redelivery of a completed task is also Acked with zero side effects.
	okXcoder := &mockVideoTranscoder{progressCalls: []float64{100}}
	svc.transcoder = okXcoder
	queued := &models.TranscodingTask{ID: taskID, ContentID: "content-retry", Profile: "720p", InputURL: "/tmp/in.mp4"}
	// Re-drive: claim is impossible (attempt cap), so drive the completed
	// branch directly by completing the row through the lease API first.
	db.mu.Lock()
	db.tasks[taskID].status = "completed"
	db.tasks[taskID].generation = 5
	db.mu.Unlock()
	acksBefore = queue.ackCount(taskID)
	deliverTo(svc, queue, queued)
	assert.Equal(t, acksBefore+1, queue.ackCount(taskID), "redelivered completed task is Acked")
	assert.Equal(t, 5, db.task(taskID).generation, "no re-claim for a completed task")
}

// TestAcceptance_HeartbeatRenewal exercises the heartbeat seam: renewal
// succeeds while this owner+generation holds a processing row and reports
// loss otherwise, which is what makes the worker stop and skip the Ack.
func TestAcceptance_HeartbeatRenewal(t *testing.T) {
	queue := newRecordingQueue()
	svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{})
	ctx := context.Background()

	taskID, err := svc.Transcode(ctx, "content-hb", "720p", "/tmp/in.mp4", 5, "0xOwner")
	require.NoError(t, err)

	lease, ok, err := svc.claim(ctx, taskID)
	require.NoError(t, err)
	require.True(t, ok)

	held, err := svc.renewLease(ctx, lease)
	require.NoError(t, err)
	assert.True(t, held)
	assert.Equal(t, db.dbTime().Add(defaultLeaseTTL).Unix(), db.task(taskID).leaseExpiresAt.Unix())

	// A different generation cannot renew: recovery reset the row and a new
	// claimer took over.
	stale := &taskLease{taskID: taskID, owner: "worker-B", generation: lease.generation + 1, attempt: lease.attempt}
	held, err = svc.renewLease(ctx, stale)
	require.NoError(t, err)
	assert.False(t, held, "only the current owner+generation may renew")

	// An unknown owner cannot renew either.
	alien := &taskLease{taskID: taskID, owner: "worker-Z", generation: lease.generation, attempt: lease.attempt}
	held, err = svc.renewLease(ctx, alien)
	require.NoError(t, err)
	assert.False(t, held)
}

// TestAcceptance_HandleSettleErrors surfaces settle-path failures through the
// recording queue: unknown handles and double settles are rejected.
func TestAcceptance_QueueHandleContract(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	require.NoError(t, q.Enqueue(&models.TranscodingTask{ID: "t-1", Status: "pending"}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, hA, err := q.Dequeue(ctx)
	require.NoError(t, err)

	require.NoError(t, q.Ack(hA))
	assert.Error(t, q.Ack(hA), "double Ack of the same delivery must be rejected")
	assert.Error(t, q.Nak(hA, time.Second), "settled delivery must not be re-queued")
}
