package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/service/transcoding"
	"github.com/rtcdance/streamgate/pkg/storage"
)

// SG-12 step 8 acceptance: output is written to a generation-private staging
// prefix, published atomically only after validation, and player routes
// resolve ONLY the published generation. The fixture drives the REAL
// TranscodingService (memory queue + worker) against a purpose-built fake SQL
// driver, a writable fake object store, and the real routes, so the assertions
// below are end-to-end within the process.

const genPublishDriverName = "streamgate_generation_publish"

var globalGenDB = &genFakeDB{contents: map[string]*genFakeContent{}}

func init() { sql.Register(genPublishDriverName, globalGenDB) }

type genFakeContent struct {
	url              string
	ownerID          string
	published        int64
	publishedProfile string
}

type genFakeTask struct {
	id               string
	contentID        string
	profile          string
	status           string
	progress         int
	inputURL         string
	outputURL        string
	errMsg           string
	priority         int
	ownerWallet      string
	metadata         []byte
	leaseOwner       string
	leaseExpiresAt   time.Time // zero = NULL
	generation       int
	outputGeneration int64
	attempt          int
	startedAt        time.Time
	completedAt      time.Time
}

func (t *genFakeTask) clone() *genFakeTask {
	c := *t
	return &c
}

type genFakeOutbox struct {
	id      string
	kind    string
	payload []byte
	status  string
}

func (o *genFakeOutbox) clone() *genFakeOutbox {
	c := *o
	c.payload = append([]byte(nil), o.payload...)
	return &c
}

// genFakeDB interprets exactly the statements the generation pipeline issues:
// task claim/lease/complete, outbox intents, generation allocation and the
// publish switch, plus the content-delete transaction. Transactions snapshot
// and restore on rollback so same-transaction atomicity stays observable.
type genFakeDB struct {
	mu       sync.Mutex
	now      time.Time
	tasks    map[string]*genFakeTask
	outbox   map[string]*genFakeOutbox
	contents map[string]*genFakeContent
}

func (d *genFakeDB) reset(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.now = now
	d.tasks = map[string]*genFakeTask{}
	d.outbox = map[string]*genFakeOutbox{}
	d.contents = map[string]*genFakeContent{}
}

// seedContent installs a content row (published generation 0 unless stated).
func (d *genFakeDB) seedContent(id, ownerID, url string, published int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.contents[id] = &genFakeContent{url: url, ownerID: ownerID, published: published}
}

// removeContent deletes only the contents row (simulates a concurrent hard
// delete while a worker is mid-flight).
func (d *genFakeDB) removeContent(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.contents, id)
}

func (d *genFakeDB) publishedOf(id string) (generation int64, contentID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.contents[id]
	if !ok {
		return -1, ""
	}
	return c.published, c.publishedProfile
}

func (d *genFakeDB) contentExists(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.contents[id]
	return ok
}

func (d *genFakeDB) taskStatus(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.tasks[id]; ok {
		return t.status
	}
	return ""
}

func (d *genFakeDB) Open(string) (driver.Conn, error) { return &genFakeConn{d: d}, nil }

type genFakeConn struct{ d *genFakeDB }

func (c *genFakeConn) Prepare(query string) (driver.Stmt, error) {
	return &genFakeStmt{d: c.d, query: query}, nil
}
func (c *genFakeConn) Close() error { return nil }
func (c *genFakeConn) Begin() (driver.Tx, error) {
	c.d.mu.Lock()
	snapTasks := make(map[string]*genFakeTask, len(c.d.tasks))
	for k, v := range c.d.tasks {
		snapTasks[k] = v.clone()
	}
	snapOutbox := make(map[string]*genFakeOutbox, len(c.d.outbox))
	for k, v := range c.d.outbox {
		snapOutbox[k] = v.clone()
	}
	snapContents := make(map[string]*genFakeContent, len(c.d.contents))
	for k, v := range c.d.contents {
		cp := *v
		snapContents[k] = &cp
	}
	c.d.mu.Unlock()
	return &genFakeTx{d: c.d, snapTasks: snapTasks, snapOutbox: snapOutbox, snapContents: snapContents}, nil
}

type genFakeTx struct {
	d            *genFakeDB
	snapTasks    map[string]*genFakeTask
	snapOutbox   map[string]*genFakeOutbox
	snapContents map[string]*genFakeContent
}

func (t *genFakeTx) Commit() error { return nil }

func (t *genFakeTx) Rollback() error {
	t.d.mu.Lock()
	defer t.d.mu.Unlock()
	t.d.tasks = t.snapTasks
	t.d.outbox = t.snapOutbox
	t.d.contents = t.snapContents
	return nil
}

type genFakeStmt struct {
	d     *genFakeDB
	query string
}

func (s *genFakeStmt) Close() error  { return nil }
func (s *genFakeStmt) NumInput() int { return -1 }
func (s *genFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	n, err := s.d.exec(s.query, args)
	return genFakeResult{rows: n}, err
}
func (s *genFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.d.query(s.query, args)
}

type genFakeResult struct{ rows int64 }

func (r genFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r genFakeResult) RowsAffected() (int64, error) { return r.rows, nil }

type genFakeRows struct {
	cols []string
	data [][]driver.Value
	idx  int
}

func (r *genFakeRows) Columns() []string { return r.cols }
func (r *genFakeRows) Close() error      { return nil }
func (r *genFakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.idx])
	r.idx++
	return nil
}

func genSQ(q string) string { return strings.Join(strings.Fields(q), " ") }

func genArgStr(args []driver.Value, i int) string {
	if i >= len(args) || args[i] == nil {
		return ""
	}
	if b, ok := args[i].([]byte); ok {
		return string(b)
	}
	return args[i].(string)
}

func genArgI64(args []driver.Value, i int) int64 {
	if i >= len(args) || args[i] == nil {
		return 0
	}
	switch v := args[i].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func genArgF64(args []driver.Value, i int) float64 {
	if i >= len(args) || args[i] == nil {
		return 0
	}
	if f, ok := args[i].(float64); ok {
		return f
	}
	return float64(genArgI64(args, i))
}

func (d *genFakeDB) exec(query string, args []driver.Value) (int64, error) {
	q := genSQ(query)
	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case strings.HasPrefix(q, "INSERT INTO transcoding_tasks"):
		id := genArgStr(args, 0)
		if _, exists := d.tasks[id]; exists {
			return 0, fmt.Errorf("duplicate key value violates unique constraint")
		}
		var meta []byte
		if args[10] != nil {
			meta = append([]byte(nil), args[10].([]byte)...)
		}
		created := time.Time{}
		if v, ok := args[9].(time.Time); ok {
			created = v
		}
		d.tasks[id] = &genFakeTask{
			id: id, contentID: genArgStr(args, 1), profile: genArgStr(args, 2),
			status: genArgStr(args, 3), progress: int(genArgI64(args, 4)),
			inputURL: genArgStr(args, 5), outputURL: genArgStr(args, 6),
			errMsg: genArgStr(args, 7), priority: int(genArgI64(args, 8)),
			metadata: meta, ownerWallet: genArgStr(args, 11),
		}
		_ = created
		return 1, nil

	case strings.HasPrefix(q, "INSERT INTO outbox"):
		id, kind := genArgStr(args, 0), genArgStr(args, 1)
		key := kind + ":" + id
		if _, exists := d.outbox[key]; exists {
			return 0, nil
		}
		d.outbox[key] = &genFakeOutbox{
			id: id, kind: kind,
			payload: append([]byte(nil), args[2].([]byte)...),
			status:  "pending",
		}
		return 1, nil

	case strings.Contains(q, "UPDATE outbox SET status = 'delivered'"):
		key := genArgStr(args, 1) + ":" + genArgStr(args, 0)
		row, ok := d.outbox[key]
		if !ok || row.status != "pending" {
			return 0, nil
		}
		row.status = "delivered"
		return 1, nil

	case strings.HasPrefix(q, "UPDATE outbox SET attempts"):
		key := genArgStr(args, 1) + ":" + genArgStr(args, 0)
		if _, ok := d.outbox[key]; ok {
			return 1, nil
		}
		return 0, nil

	case strings.Contains(q, "SET lease_expires_at = NOW()"):
		// renewLease: id, owner, ttl, lease generation.
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok || row.status != "processing" || row.leaseOwner != genArgStr(args, 1) || row.generation != int(genArgI64(args, 3)) {
			return 0, nil
		}
		row.leaseExpiresAt = d.now.Add(time.Duration(genArgF64(args, 2) * float64(time.Second)))
		return 1, nil

	case strings.Contains(q, "SET status = 'pending', last_error"):
		// releaseForRetry: id, owner, cause, delay, lease generation.
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok || row.status != "processing" || row.leaseOwner != genArgStr(args, 1) || row.generation != int(genArgI64(args, 4)) {
			return 0, nil
		}
		row.status = "pending"
		row.errMsg = ""
		row.leaseOwner = ""
		row.leaseExpiresAt = time.Time{}
		return 1, nil

	case strings.Contains(q, "SET status = 'failed'"):
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok || (row.status != "pending" && row.status != "processing") {
			return 0, nil
		}
		if len(args) >= 4 && (row.leaseOwner != genArgStr(args, 2) || row.generation != int(genArgI64(args, 3))) {
			return 0, nil
		}
		row.status = "failed"
		row.errMsg = genArgStr(args, 1)
		row.completedAt = d.now
		return 1, nil

	case strings.Contains(q, "SET status = 'pending', lease_owner = '', lease_expires_at = NULL"):
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
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok || row.status != "processing" {
			return 0, nil
		}
		// args: id, outputURL, completedAt, [metadata], [owner, lease generation]
		if len(args) >= 5 && (row.leaseOwner != genArgStr(args, len(args)-2) || row.generation != int(genArgI64(args, len(args)-1))) {
			return 0, nil
		}
		row.status = "completed"
		row.progress = 100
		row.outputURL = genArgStr(args, 1)
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
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok {
			return 0, nil
		}
		if len(args) >= 5 && (row.leaseOwner != genArgStr(args, 3) || row.generation != int(genArgI64(args, 4))) {
			return 0, nil
		}
		row.progress = int(genArgI64(args, 1))
		if args[2] != nil {
			row.metadata = append([]byte(nil), args[2].([]byte)...)
		}
		return 1, nil

	case strings.Contains(q, "SET progress = $2 WHERE id"):
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok {
			return 0, nil
		}
		if len(args) >= 4 && (row.leaseOwner != genArgStr(args, 2) || row.generation != int(genArgI64(args, 3))) {
			return 0, nil
		}
		row.progress = int(genArgI64(args, 1))
		return 1, nil

	case strings.Contains(q, "SET status = $2, error = $3, completed_at"):
		// FailTask.
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok || (row.status != "pending" && row.status != "processing") {
			return 0, nil
		}
		row.status = genArgStr(args, 1)
		row.errMsg = genArgStr(args, 2)
		if v, ok := args[3].(time.Time); ok {
			row.completedAt = v
		}
		return 1, nil

	case strings.HasPrefix(q, "UPDATE transcoding_tasks") && strings.Contains(q, "SET status = 'cancelled'"):
		// Content delete: cancel every active task of that content.
		var n int64
		for _, row := range d.tasks {
			if row.contentID != genArgStr(args, 0) || (row.status != "pending" && row.status != "processing") {
				continue
			}
			row.status = "cancelled"
			row.completedAt = d.now
			row.leaseOwner = ""
			row.leaseExpiresAt = time.Time{}
			n++
		}
		return n, nil

	case strings.HasPrefix(q, "DELETE FROM content_metadata"):
		return 1, nil

	case strings.HasPrefix(q, "DELETE FROM contents"):
		row, ok := d.contents[genArgStr(args, 0)]
		if !ok || row.ownerID != genArgStr(args, 1) {
			return 0, nil
		}
		delete(d.contents, genArgStr(args, 0))
		return 1, nil

	case strings.HasPrefix(q, "UPDATE contents"):
		// Content status / thumbnail updates: a missing row is a silent no-op,
		// exactly like Postgres.
		if _, ok := d.contents[genArgStr(args, 0)]; !ok {
			return 0, nil
		}
		return 1, nil
	}
	return 0, fmt.Errorf("genFakeDB: unhandled exec: %s", q)
}

func (d *genFakeDB) claimLocked(args []driver.Value) (int64, error) {
	row, ok := d.tasks[genArgStr(args, 0)]
	maxAttempt := int(genArgI64(args, 3))
	claimable := ok && row.attempt < maxAttempt &&
		(row.status == "pending" ||
			(row.status == "processing" && !row.leaseExpiresAt.IsZero() && row.leaseExpiresAt.Before(d.now)))
	if !claimable {
		return 0, nil
	}
	row.status = "processing"
	row.leaseOwner = genArgStr(args, 1)
	row.leaseExpiresAt = d.now.Add(time.Duration(genArgF64(args, 2) * float64(time.Second)))
	row.generation++
	row.attempt++
	row.startedAt = d.now
	return 1, nil
}

func (d *genFakeDB) query(query string, args []driver.Value) (driver.Rows, error) {
	q := genSQ(query)
	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case strings.Contains(q, "SELECT status, attempt,"):
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"status", "attempt", "lease_valid"}}, nil
		}
		leaseValid := row.status == "processing" && !row.leaseExpiresAt.IsZero() && row.leaseExpiresAt.After(d.now)
		return &genFakeRows{
			cols: []string{"status", "attempt", "lease_valid"},
			data: [][]driver.Value{{row.status, int64(row.attempt), leaseValid}},
		}, nil

	case strings.Contains(q, "RETURNING generation, attempt"):
		n, err := d.claimLocked(args)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return &genFakeRows{cols: []string{"generation", "attempt"}}, nil
		}
		row := d.tasks[genArgStr(args, 0)]
		return &genFakeRows{
			cols: []string{"generation", "attempt"},
			data: [][]driver.Value{{int64(row.generation), int64(row.attempt)}},
		}, nil

	case strings.Contains(q, "RETURNING output_generation"):
		// Generation allocation: id ($1), next ($2), only when still unallocated.
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok || row.outputGeneration != 0 {
			return &genFakeRows{cols: []string{"output_generation"}}, nil
		}
		row.outputGeneration = genArgI64(args, 1)
		return &genFakeRows{
			cols: []string{"output_generation"},
			data: [][]driver.Value{{row.outputGeneration}},
		}, nil

	case strings.Contains(q, "SELECT id, kind, payload FROM outbox"):
		var data [][]driver.Value
		for _, key := range genSortedOutboxKeys(d.outbox) {
			row := d.outbox[key]
			if row.status == "pending" {
				data = append(data, []driver.Value{row.id, row.kind, row.payload})
			}
			if len(data) >= 25 {
				break
			}
		}
		return &genFakeRows{cols: []string{"id", "kind", "payload"}, data: data}, nil

	case strings.Contains(q, "SELECT payload, status FROM outbox"):
		row, ok := d.outbox[genArgStr(args, 1)+":"+genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"payload", "status"}}, nil
		}
		return &genFakeRows{cols: []string{"payload", "status"}, data: [][]driver.Value{{row.payload, row.status}}}, nil

	case strings.Contains(q, "SELECT content_id, profile, output_generation FROM transcoding_tasks WHERE id"):
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"content_id", "profile", "output_generation"}}, nil
		}
		return &genFakeRows{
			cols: []string{"content_id", "profile", "output_generation"},
			data: [][]driver.Value{{row.contentID, row.profile, row.outputGeneration}},
		}, nil

	case strings.Contains(q, "SELECT content_id, output_generation FROM transcoding_tasks WHERE id"):
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"content_id", "output_generation"}}, nil
		}
		return &genFakeRows{
			cols: []string{"content_id", "output_generation"},
			data: [][]driver.Value{{row.contentID, row.outputGeneration}},
		}, nil

	case strings.Contains(q, "SELECT output_generation FROM transcoding_tasks WHERE id"):
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"output_generation"}}, nil
		}
		return &genFakeRows{cols: []string{"output_generation"}, data: [][]driver.Value{{row.outputGeneration}}}, nil

	case strings.Contains(q, "SELECT COALESCE(MAX(output_generation), 0) FROM transcoding_tasks WHERE content_id"):
		var highest int64
		for _, row := range d.tasks {
			if row.contentID == genArgStr(args, 0) && row.outputGeneration > highest {
				highest = row.outputGeneration
			}
		}
		return &genFakeRows{cols: []string{"max"}, data: [][]driver.Value{{highest}}}, nil

	case strings.Contains(q, "SELECT COUNT(*) FROM transcoding_tasks WHERE content_id"):
		var n int64
		for _, row := range d.tasks {
			if row.contentID == genArgStr(args, 0) && (row.status == "pending" || row.status == "processing") {
				n++
			}
		}
		return &genFakeRows{cols: []string{"count"}, data: [][]driver.Value{{n}}}, nil

	case strings.Contains(q, "SELECT id, content_id, profile, status, progress"):
		row, ok := d.tasks[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"id"}}, nil
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
		return &genFakeRows{
			cols: []string{"id", "content_id", "profile", "status", "progress", "input_url", "output_url", "error", "priority", "created_at", "started_at", "completed_at", "metadata", "owner_wallet"},
			data: [][]driver.Value{{
				row.id, row.contentID, row.profile, row.status, int64(row.progress),
				row.inputURL, row.outputURL, row.errMsg, int64(row.priority),
				d.now, started, completed, meta, row.ownerWallet,
			}},
		}, nil

	case strings.Contains(q, "SELECT url FROM contents WHERE id"):
		c, ok := d.contents[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"url"}}, nil
		}
		return &genFakeRows{cols: []string{"url"}, data: [][]driver.Value{{c.url}}}, nil

	case strings.Contains(q, "SELECT id FROM contents WHERE id = $1 FOR UPDATE"):
		if _, ok := d.contents[genArgStr(args, 0)]; !ok {
			return &genFakeRows{cols: []string{"id"}}, nil
		}
		return &genFakeRows{cols: []string{"id"}, data: [][]driver.Value{{genArgStr(args, 0)}}}, nil

	case strings.Contains(q, "WITH prev AS (SELECT published_generation"):
		// Publish switch: contentID ($1), generation ($2), profile ($3).
		contentID := genArgStr(args, 0)
		generation := genArgI64(args, 1)
		c, ok := d.contents[contentID]
		if !ok || c.published >= generation {
			return &genFakeRows{cols: []string{"g"}}, nil
		}
		prev := c.published
		c.published = generation
		c.publishedProfile = genArgStr(args, 2)
		return &genFakeRows{cols: []string{"g"}, data: [][]driver.Value{{prev}}}, nil

	case strings.Contains(q, "SELECT published_generation, published_profile FROM contents WHERE id"):
		c, ok := d.contents[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"published_generation", "published_profile"}}, nil
		}
		return &genFakeRows{
			cols: []string{"published_generation", "published_profile"},
			data: [][]driver.Value{{c.published, c.publishedProfile}},
		}, nil

	case strings.Contains(q, "SELECT published_generation FROM contents WHERE id"):
		c, ok := d.contents[genArgStr(args, 0)]
		if !ok {
			return &genFakeRows{cols: []string{"published_generation"}}, nil
		}
		return &genFakeRows{cols: []string{"published_generation"}, data: [][]driver.Value{{c.published}}}, nil
	}
	return nil, fmt.Errorf("genFakeDB: unhandled query: %s", q)
}

func genSortedOutboxKeys(m map[string]*genFakeOutbox) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// genFakeStorageDB adapts *sql.DB (fake driver) to storage.DB.
type genFakeStorageDB struct{ sqlDB *sql.DB }

func (f *genFakeStorageDB) Query(ctx context.Context, query string, args ...interface{}) (storage.Rows, error) {
	return f.sqlDB.QueryContext(ctx, query, args...)
}
func (f *genFakeStorageDB) QueryRow(ctx context.Context, query string, args ...interface{}) *storage.CancelRow {
	return storage.NewCancelRow(f.sqlDB.QueryRowContext(ctx, query, args...), func() {})
}
func (f *genFakeStorageDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return f.sqlDB.ExecContext(ctx, query, args...)
}
func (f *genFakeStorageDB) Begin(ctx context.Context) (*sql.Tx, error) {
	return f.sqlDB.BeginTx(ctx, nil)
}
func (f *genFakeStorageDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
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
func (f *genFakeStorageDB) Ping(context.Context) error { return nil }
func (f *genFakeStorageDB) Close() error               { return f.sqlDB.Close() }

// genStore is a writable fake object store: uploads land in memory, listings
// are recorded so tests can assert WHICH prefix was resolved.
type genStore struct {
	mu             sync.Mutex
	objects        map[string][]byte
	listedPrefixes []string
}

func newGenStore() *genStore {
	return &genStore{objects: map[string][]byte{}}
}

func (s *genStore) put(key, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = []byte(content)
}

func (s *genStore) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[key]
	return ok
}

func (s *genStore) hasPrefix(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func (s *genStore) listed(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.listedPrefixes {
		if p == prefix {
			return true
		}
	}
	return false
}

func (s *genStore) Upload(ctx context.Context, bucket, objectName string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[objectName] = append([]byte(nil), data...)
	return nil
}
func (s *genStore) UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return s.Upload(ctx, bucket, objectName, data)
}
func (s *genStore) UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error {
	return s.Upload(ctx, bucket, objectName, data)
}
func (s *genStore) UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	return s.UploadStream(ctx, bucket, objectName, reader, size)
}
func (s *genStore) Download(ctx context.Context, bucket, objectName string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.objects[objectName]; ok {
		return append([]byte(nil), data...), nil
	}
	return nil, fmt.Errorf("object %s not found", objectName)
}
func (s *genStore) DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error) {
	data, err := s.Download(ctx, bucket, objectName)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}
func (s *genStore) Delete(ctx context.Context, bucket, objectName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, objectName)
	return nil
}
func (s *genStore) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listedPrefixes = append(s.listedPrefixes, prefix)
	var keys []string
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}
func (s *genStore) Exists(ctx context.Context, bucket, objectName string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[objectName]
	return ok, nil
}

// genTranscoder writes HLS outputs to the task's private dir. Fields are
// mutated between tasks (never while one runs) and read under the mutex.
type genTranscoder struct {
	mu           sync.Mutex
	payload      string
	playlistOnly bool // writes a playlist but no .ts: validation must fail
	gate         chan struct{}
}

const genMediaPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:8
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:8.333333,
640x360_000.ts
#EXTINF:1.666667,
640x360_001.ts
#EXT-X-ENDLIST
`

const stagingMarker = "STAGING-SECRET-UNPUBLISHED"

func (f *genTranscoder) configure(payload string, playlistOnly bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payload = payload
	f.playlistOnly = playlistOnly
}

func (f *genTranscoder) openGate() chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = make(chan struct{})
	return f.gate
}

func (f *genTranscoder) closeGate() {
	f.mu.Lock()
	gate := f.gate
	f.gate = nil
	f.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}

func (f *genTranscoder) TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(variant string, progress float64)) error {
	f.mu.Lock()
	payload := f.payload
	playlistOnly := f.playlistOnly
	gate := f.gate
	f.mu.Unlock()

	progressFn("", 10)
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	progressFn("", 100)

	files := map[string]string{
		"640x360.m3u8": genMediaPlaylist,
	}
	if !playlistOnly {
		files["640x360_000.ts"] = payload + "-seg0"
		files["640x360_001.ts"] = payload + "-seg1"
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(outputDir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

const genTestWallet = "0x1234567890abcdef1234567890abcdef12345678"

type genPublishFixture struct {
	fake       *genFakeDB
	store      *genStore
	xcoder     *genTranscoder
	svc        *transcoding.TranscodingService
	router     *gin.Engine
	authSvc    *service.AuthService
	cache      *StreamingCache
	contentSvc *service.ContentService
	hookFired  chan string
}

func newGenPublishFixture(t *testing.T) *genPublishFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	globalGenDB.reset(time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC))
	sqlDB, err := sql.Open(genPublishDriverName, "")
	require.NoError(t, err)
	db := &genFakeStorageDB{sqlDB: sqlDB}
	store := newGenStore()
	xcoder := &genTranscoder{payload: "GEN1"}
	queue := transcoding.NewMemoryTranscodingQueue()
	streamCache := NewStreamingCache()

	svc := transcoding.NewTranscodingService(db, queue,
		transcoding.WithTranscoder(xcoder),
		transcoding.WithStorage(store),
		transcoding.WithLogger(zap.NewNop()),
		transcoding.WithLeaseOwner("gen-worker-A"),
		transcoding.WithRetryDelays(30*time.Millisecond, 30*time.Millisecond),
		transcoding.WithMinWorkers(1),
	)
	hookFired := make(chan string, 16)
	// Mirrors the gateway's PostTranscodeHook: a publish invalidates every
	// cached answer for that content in this process.
	svc.RegisterPostTranscodeHook(func(_ context.Context, contentID, _, _ string) {
		streamCache.Invalidate(contentID)
		select {
		case hookFired <- contentID:
		default:
		}
	})
	svc.StartWorker(zap.NewNop())

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("wallet_address", genTestWallet)
		c.Next()
	})
	authSvc := newGatewayTestAuthService()
	streamingSvc := service.NewStreamingService(nil, nil, nil, "http://localhost")
	limiter := newStreamLimiter(100)
	resolver := newPublishedGenerationResolver(db)
	RegisterStreamingSegmentRoute(router, zap.NewNop(), authSvc, store, limiter, streamCache, resolver)
	RegisterStreamingRoutes(router, zap.NewNop(), authSvc, streamingSvc, store, limiter, streamCache, resolver)

	f := &genPublishFixture{
		fake:       globalGenDB,
		store:      store,
		xcoder:     xcoder,
		svc:        svc,
		router:     router,
		authSvc:    authSvc,
		cache:      streamCache,
		contentSvc: service.NewContentService(db, nil, nil, zap.NewNop()),
		hookFired:  hookFired,
	}
	t.Cleanup(func() {
		xcoder.closeGate()
		svc.StopWorker()
		_ = sqlDB.Close()
	})
	return f
}

func (f *genPublishFixture) submit(t *testing.T, contentID, profile string) string {
	t.Helper()
	taskID, err := f.svc.Transcode(context.Background(), contentID, profile, "/tmp/input.mp4", 5, "0xOwner")
	require.NoError(t, err)
	return taskID
}

func (f *genPublishFixture) waitTaskStatus(t *testing.T, taskID, want string) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, err := f.svc.GetTranscodingStatus(context.Background(), taskID)
		return err == nil && got.Status == want
	}, 20*time.Second, 10*time.Millisecond, "task %s should reach status %q", taskID, want)
}

// waitHook blocks until the post-transcode hook ran for this content, which is
// the moment invalidation and any post-commit cleanup are guaranteed done.
func (f *genPublishFixture) waitHook(t *testing.T, contentID string) {
	t.Helper()
	select {
	case got := <-f.hookFired:
		require.Equal(t, contentID, got)
	case <-time.After(20 * time.Second):
		t.Fatalf("post-transcode hook did not fire for %s", contentID)
	}
}

func (f *genPublishFixture) runToCompletion(t *testing.T, contentID, profile string) string {
	t.Helper()
	taskID := f.submit(t, contentID, profile)
	f.waitTaskStatus(t, taskID, "completed")
	f.waitHook(t, contentID)
	return taskID
}

func (f *genPublishFixture) token(t *testing.T, contentID string) string {
	t.Helper()
	token, err := f.authSvc.GeneratePlaybackToken(context.Background(),
		genTestWallet, contentID, "", "", 1, 5*time.Minute, "")
	require.NoError(t, err)
	return token
}

func (f *genPublishFixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	f.router.ServeHTTP(w, req)
	return w
}

// genManifestPath returns the master manifest route for a content.
func genManifestPath(contentID string) string {
	return APIPrefix + "/streaming/" + contentID + "/manifest.m3u8"
}

func genSegmentPath(contentID, segName, quality, token string) string {
	u := APIPrefix + "/streaming/" + contentID + "/segment/" + segName + "?playback_token=" + token
	if quality != "" {
		u += "&quality=" + quality
	}
	return u
}

func genVariantManifestPath(contentID, quality, token string) string {
	return genManifestPath(contentID) + "?quality=" + quality + "&playback_token=" + token
}

// TestAcceptance_TaskGenerationPublish pins SG-12 step 8: output is staged
// under a generation-private prefix, published by one atomic lease-guarded DB
// switch only after validation, and player routes resolve ONLY the published
// generation — a valid content capability must never read a guessed
// staging/failed path.
func TestAcceptance_TaskGenerationPublish(t *testing.T) {
	t.Run("a_upload_complete_publishes_generation_1_and_routes_serve_g1", func(t *testing.T) {
		f := newGenPublishFixture(t)
		f.fake.seedContent("gen-a", genTestWallet, "", 0)

		f.runToCompletion(t, "gen-a", "abr")

		// The publish flipped the row...
		published, profile := f.fake.publishedOf("gen-a")
		assert.Equal(t, int64(1), published, "first completed task must publish generation 1")
		assert.Equal(t, "abr", profile)

		// ...upload happened under the private staging prefix, never flat...
		assert.True(t, f.store.has("streams/gen-a/g1/640x360/640x360.m3u8"))
		assert.True(t, f.store.has("streams/gen-a/g1/640x360/640x360_000.ts"))
		assert.False(t, f.store.hasPrefix("streams/gen-a/640x360/"), "nothing may be written flat")

		// ...validation listed exactly that prefix...
		assert.True(t, f.store.listed("streams/gen-a/g1/"), "artifact validation must list the generation prefix")

		// ...and the manifest route resolves playlist keys carrying the prefix.
		token := f.token(t, "gen-a")
		w := f.get(t, genVariantManifestPath("gen-a", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		playlists, masterKey, ok := f.cache.GetStoredPlaylists("gen-a", 1)
		require.True(t, ok, "segment index must be cached for generation 1")
		require.Equal(t, "streams/gen-a/g1/640x360/640x360.m3u8", playlists["640x360"])
		_ = masterKey
		assert.Contains(t, w.Body.String(), "#EXTINF:8.333333,", "the stored playlist must be served")

		// The segment route serves the bytes of the published generation.
		w = f.get(t, genSegmentPath("gen-a", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "GEN1-seg0", w.Body.String())
	})

	t.Run("b_mid_flight_generation_is_not_servable", func(t *testing.T) {
		f := newGenPublishFixture(t)
		// Published generation 1...
		f.fake.seedContent("gen-b", genTestWallet, "", 1)
		f.store.put("streams/gen-b/g1/640x360/640x360.m3u8", genMediaPlaylist)
		f.store.put("streams/gen-b/g1/640x360/640x360_000.ts", "G1-BYTES-seg0")
		// ...and an allocated but NOT published generation 2 mid-flight.
		f.store.put("streams/gen-b/g2/640x360/640x360.m3u8",
			"#EXTM3U\n#EXTINF:99.99,\n640x360_000.ts\n#EXT-X-ENDLIST\n")
		f.store.put("streams/gen-b/g2/640x360/640x360_000.ts", stagingMarker+"-g2")
		f.store.put("streams/gen-b/g2/640x360/staging_000.ts", stagingMarker+"-only")

		token := f.token(t, "gen-b")

		// The manifest resolves generation 1 only.
		w := f.get(t, genVariantManifestPath("gen-b", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "#EXTINF:8.333333,")
		assert.NotContains(t, w.Body.String(), "99.99", "the unpublished generation's playlist must not be served")
		assert.NotContains(t, w.Body.String(), stagingMarker)

		// A guessed staging quality resolves to nothing: no media bytes.
		w = f.get(t, genVariantManifestPath("gen-b", "g2", token))
		assert.Equal(t, http.StatusNotFound, w.Code, "a guessed generation quality must not resolve")
		assert.NotContains(t, w.Body.String(), stagingMarker)

		// A guessed staging-only segment name resolves to nothing either.
		w = f.get(t, genSegmentPath("gen-b", "staging_000.ts", "640x360", token))
		assert.NotEqual(t, http.StatusOK, w.Code, "a staging-only segment must never be served")
		assert.NotContains(t, w.Body.String(), stagingMarker)

		// Even a segment name that exists in both generations serves gen 1.
		w = f.get(t, genSegmentPath("gen-b", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "G1-BYTES-seg0", w.Body.String())
	})

	t.Run("c_failed_task_cleans_only_its_own_prefix", func(t *testing.T) {
		f := newGenPublishFixture(t)
		f.fake.seedContent("gen-c", genTestWallet, "", 0)

		// First transcode publishes generation 1 and serves.
		f.runToCompletion(t, "gen-c", "abr")
		token := f.token(t, "gen-c")
		w := f.get(t, genSegmentPath("gen-c", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "GEN1-seg0", w.Body.String())

		// Second transcode writes a playlist but no segment: validation fails,
		// the task exhausts its attempts and terminates. Its staging must be
		// cleaned; the published generation must keep serving untouched.
		f.xcoder.configure("GEN2", true)
		taskID := f.submit(t, "gen-c", "abr")
		f.waitTaskStatus(t, taskID, "failed")
		require.Eventually(t, func() bool {
			return !f.store.hasPrefix("streams/gen-c/g2/")
		}, 10*time.Second, 20*time.Millisecond, "the failed task must clean its own staging prefix")
		f.xcoder.configure("GEN1", false)

		assert.True(t, f.store.hasPrefix("streams/gen-c/g1/"), "the published generation must not be touched")
		published, _ := f.fake.publishedOf("gen-c")
		assert.Equal(t, int64(1), published, "a failure must never change the published generation")

		// Still serving from generation 1.
		w = f.get(t, genVariantManifestPath("gen-c", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		w = f.get(t, genSegmentPath("gen-c", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "GEN1-seg0", w.Body.String())
	})

	t.Run("d_retranscode_publishes_generation_2_and_cleans_the_old", func(t *testing.T) {
		f := newGenPublishFixture(t)
		f.fake.seedContent("gen-d", genTestWallet, "", 0)

		// Generation 1 published; prime the caches by serving it.
		f.xcoder.configure("GEN1", false)
		f.runToCompletion(t, "gen-d", "abr")
		token := f.token(t, "gen-d")
		w := f.get(t, genSegmentPath("gen-d", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "GEN1-seg0", w.Body.String())

		// Re-transcode (same input, new run): generation 2 publishes and the
		// old generation's objects are cleaned best-effort.
		f.xcoder.configure("GEN2", false)
		f.runToCompletion(t, "gen-d", "abr")

		published, profile := f.fake.publishedOf("gen-d")
		assert.Equal(t, int64(2), published, "re-transcode must publish generation 2")
		assert.Equal(t, "abr", profile)
		assert.True(t, f.store.hasPrefix("streams/gen-d/g2/"))
		assert.False(t, f.store.hasPrefix("streams/gen-d/g1/"), "the superseded generation must be cleaned")

		// Routes now serve generation 2 (hook invalidation already happened).
		w = f.get(t, genVariantManifestPath("gen-d", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), "99.99")
		w = f.get(t, genSegmentPath("gen-d", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "GEN2-seg0", w.Body.String(), "routes must serve the newly published generation")
		f.xcoder.configure("GEN1", false)
	})

	t.Run("e_legacy_generation_0_flat_layout_still_serves", func(t *testing.T) {
		f := newGenPublishFixture(t)
		// Legacy content: generation 0, flat keys exactly as step 9 left them.
		f.fake.seedContent("gen-e", genTestWallet, "", 0)
		f.store.put("streams/gen-e/640x360/640x360.m3u8", genMediaPlaylist)
		f.store.put("streams/gen-e/640x360/640x360_000.ts", "LEGACY-seg0")

		token := f.token(t, "gen-e")
		w := f.get(t, genVariantManifestPath("gen-e", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code, "legacy flat content must keep serving")
		assert.Contains(t, w.Body.String(), "#EXTINF:8.333333,")

		w = f.get(t, genSegmentPath("gen-e", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "LEGACY-seg0", w.Body.String())

		// A staging prefix coexisting with a legacy content stays invisible:
		// generation 0 listings skip generation directories.
		f.store.put("streams/gen-e/g7/640x360/640x360_000.ts", stagingMarker)
		w = f.get(t, genVariantManifestPath("gen-e", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), stagingMarker)
		w = f.get(t, genSegmentPath("gen-e", "640x360_000.ts", "640x360", token))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "LEGACY-seg0", w.Body.String())
	})

	t.Run("f_delete_cancels_tasks_and_completion_publish_becomes_noop", func(t *testing.T) {
		f := newGenPublishFixture(t)
		f.fake.seedContent("gen-f", genTestWallet, "", 0)

		// Two active tasks: one claimed and blocked mid-transcode, one pending.
		f.xcoder.openGate()
		processing := f.submit(t, "gen-f", "abr")
		f.waitTaskStatus(t, processing, "processing")
		pending := f.submit(t, "gen-f", "720p")
		f.waitTaskStatus(t, pending, "pending")

		// Deleting the content cancels both active tasks in one transaction.
		require.NoError(t, f.contentSvc.DeleteContentWithTx(context.Background(), "gen-f", genTestWallet))
		assert.Equal(t, "cancelled", f.fake.taskStatus(processing), "an in-flight task must be cancelled")
		assert.Equal(t, "cancelled", f.fake.taskStatus(pending), "a pending task must be cancelled")
		assert.False(t, f.fake.contentExists("gen-f"))

		// Release the old worker: it keeps running past the cancel, but it
		// must never complete its task, republish, or resurrect the row.
		f.xcoder.closeGate()
		assert.Never(t, func() bool {
			return f.fake.taskStatus(processing) == "completed"
		}, 2*time.Second, 50*time.Millisecond, "a cancelled task must never complete")
		assert.False(t, f.fake.contentExists("gen-f"), "completion must not resurrect a deleted content")

		// Nothing is servable for the deleted content: the resolver finds no
		// row (generation 0) and the flat listing skips generation dirs.
		token := f.token(t, "gen-f")
		w := f.get(t, genVariantManifestPath("gen-f", "640x360", token))
		assert.NotEqual(t, http.StatusOK, w.Code, "deleted content must not serve media")
		assert.NotContains(t, w.Body.String(), "GEN1-seg0")

		// Publish no-op variant: the row disappears while a worker is
		// mid-flight and the task itself is NOT cancelled (simulated hard
		// delete). Completion commits, publish flips nothing, and the task's
		// own staging is cleaned.
		f.fake.seedContent("gen-g", genTestWallet, "", 0)
		f.xcoder.openGate()
		taskG := f.submit(t, "gen-g", "abr")
		f.waitTaskStatus(t, taskG, "processing")
		f.fake.removeContent("gen-g")
		f.xcoder.closeGate()
		f.waitTaskStatus(t, taskG, "completed")
		f.waitHook(t, "gen-g")

		assert.False(t, f.fake.contentExists("gen-g"), "publish must be a no-op for a deleted content")
		require.Eventually(t, func() bool {
			return !f.store.hasPrefix("streams/gen-g/g1/")
		}, 10*time.Second, 20*time.Millisecond, "an unpublished generation must be cleaned after the no-op")
		w = f.get(t, genVariantManifestPath("gen-g", "640x360", token))
		assert.NotEqual(t, http.StatusOK, w.Code, "the no-op published nothing to serve")
		assert.NotContains(t, w.Body.String(), "GEN1-seg0")
	})
}
