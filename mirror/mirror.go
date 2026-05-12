// Package mirror provides a resource-agnostic local SQLite cache for
// agent-native CLIs. It complements the live API path: a CLI mirrors
// upstream resources into SQLite so subsequent reads are token-cheap,
// offline-tolerant, and — most importantly — composable across resources
// (compound queries, joins, FTS5 search) in ways the upstream API
// usually can't express.
//
// Design notes:
//
//   - Generic schema, not codegen. The petstore reference (Apache-2.0,
//     CLI Printing Press) generates a per-resource typed table per spec.
//     Here we ship one shared `resources` table keyed by (resource_name, id)
//     plus an FTS5 index, and let callers declare optional typed columns
//     via Resource.Columns that we mirror to a per-resource side table.
//   - Pure-Go driver (modernc.org/sqlite) so consumers cross-compile
//     without CGO.
//   - Single-process write serialization via a sync.Mutex; reads bypass
//     the lock and run concurrently against WAL.
//
// Open the mirror, Register each resource family, then Upsert/Search/etc.
// Use DB() for ad-hoc compound queries.
package mirror

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SchemaVersion is stamped into PRAGMA user_version on first open and
// checked on every subsequent open. An older binary opening a newer DB
// returns ErrSchemaTooNew rather than silently misreading.
const SchemaVersion = 1

// ErrSchemaTooNew is returned when the on-disk database was written by a
// newer SchemaVersion than this binary supports.
var ErrSchemaTooNew = fmt.Errorf("mirror: database schema is newer than this binary supports")

// validIdentifier pins resource names, column names, and column types to a
// safe SQL-identifier shape before any Sprintf interpolation. Resource and
// column names also flow through this gate at Register() time.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// allowedColumnTypes is the closed set of SQLite column types Register
// accepts. Keeps the typed-column path away from arbitrary type strings
// and the SQL-injection surface that comes with them.
var allowedColumnTypes = map[string]bool{
	"TEXT":     true,
	"INTEGER":  true,
	"REAL":     true,
	"NUMERIC":  true,
	"BLOB":     true,
	"DATETIME": true,
	"BOOLEAN":  true,
}

// defaultIDFields is the fallback ID-extraction order when a Resource
// declares no IDFields. Matches the petstore reference's
// genericIDFieldFallbacks for behavioral parity.
var defaultIDFields = []string{"id", "ID", "uuid", "slug", "name"}

// Mirror is a SQLite-backed local cache.
type Mirror struct {
	db   *sql.DB
	mu   sync.Mutex // serializes writes; reads bypass via WAL
	path string

	resMu sync.RWMutex
	res   map[string]Resource
}

// Resource declares one resource family. The mirror dispatches Upsert /
// Get / Search calls by Name; an unregistered name returns an error.
type Resource struct {
	// Name is the unique resource family identifier (also used as the
	// physical typed-table suffix when Columns is non-empty). Must match
	// validIdentifier — letters, digits, underscores; must not start with
	// a digit.
	Name string

	// IDFields is the ordered fallback list of JSON keys to try when
	// extracting a primary key from an item. The first key whose lookup
	// returns a non-empty value wins. Defaults to
	// {"id","ID","uuid","slug","name"} when unset.
	IDFields []string

	// Columns optionally declares typed columns mirrored to a per-resource
	// side table named "rt_<Name>". Each column's value is extracted from
	// the upserted JSON via the column's From path on every write. The
	// generic resources row is always written; the typed table is written
	// in the same transaction when Columns is non-empty.
	Columns []Column
}

// Column declares one typed column on a resource's side table.
type Column struct {
	// Name is the SQL column name. Must match validIdentifier.
	Name string

	// Type is the SQLite column type. Restricted to a known-safe set
	// (see allowedColumnTypes) since it is splice into DDL.
	Type string

	// From is the JSON key (top-level) to extract the column value from.
	// Tries snake_case (as given) first, then camelCase rendering second
	// — same lookup convention as the petstore reference.
	From string

	// Index causes Register to create an index on this column.
	Index bool
}

// ResourceStatus is the per-resource summary returned by Status.
type ResourceStatus struct {
	Count        int       `json:"count"`
	LastSyncedAt time.Time `json:"last_synced_at,omitempty"`
	LastCursor   string    `json:"last_cursor,omitempty"`
}

// Open opens or creates the mirror at dbPath using the background
// context. Use OpenWithContext from a Cobra command so SIGINT during
// migration interrupts cleanly.
func Open(dbPath string) (*Mirror, error) {
	return OpenWithContext(context.Background(), dbPath)
}

// OpenWithContext opens or creates the mirror at dbPath. The special
// path ":memory:" creates an in-process database — useful for tests.
func OpenWithContext(ctx context.Context, dbPath string) (*Mirror, error) {
	if dbPath != ":memory:" {
		if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("mirror: creating db directory: %w", err)
			}
		}
	}

	dsn := dbPath + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=ON&_temp_store=MEMORY"
	if dbPath == ":memory:" {
		// :memory: + WAL is meaningless; keep busy_timeout for parity but
		// drop journal/synchronous tuning that only applies to file DBs.
		dsn = dbPath + "?_busy_timeout=5000&_foreign_keys=ON"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("mirror: opening database: %w", err)
	}

	// modernc.org/sqlite + ":memory:" gives each connection its own private
	// database. Pin to a single connection so tests see what they wrote.
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(2)
	}

	m := &Mirror{db: db, path: dbPath, res: map[string]Resource{}}
	if err := m.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("mirror: running migrations: %w", err)
	}
	return m, nil
}

// Close closes the underlying database connection.
func (m *Mirror) Close() error { return m.db.Close() }

// Path returns the on-disk path of the database (":memory:" for in-memory).
func (m *Mirror) Path() string { return m.path }

// DB exposes the underlying *sql.DB for ad-hoc compound queries (joins
// across resources, FTS5 + typed-column hybrid filters, analytics rollups).
// Callers must not Close the returned handle.
func (m *Mirror) DB() *sql.DB { return m.db }

func (m *Mirror) migrate(ctx context.Context) error {
	var current int
	if err := m.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if current > SchemaVersion {
		return fmt.Errorf("%w: on-disk %d, supported %d", ErrSchemaTooNew, current, SchemaVersion)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS resources (
			resource_name TEXT NOT NULL,
			id TEXT NOT NULL,
			data JSON NOT NULL,
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (resource_name, id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_resources_name ON resources(resource_name)`,
		`CREATE INDEX IF NOT EXISTS idx_resources_synced ON resources(synced_at)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS resources_fts USING fts5(
			id, resource_name, content, tokenize='porter unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS sync_state (
			resource_name TEXT PRIMARY KEY,
			last_cursor TEXT,
			last_synced_at DATETIME,
			total_count INTEGER DEFAULT 0
		)`,
	}
	for _, s := range stmts {
		if _, err := m.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	if _, err := m.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion)); err != nil {
		return fmt.Errorf("stamp user_version: %w", err)
	}
	return nil
}

// Register declares a Resource. It is idempotent: registering the same
// Resource (same Name, same Columns shape) twice is a no-op. Columns may
// only be added across calls — re-registering with a column removed
// returns an error. Re-registering with a different column Type or
// Index flag also returns an error.
func (m *Mirror) Register(r Resource) error {
	if !validIdentifier.MatchString(r.Name) {
		return fmt.Errorf("mirror: invalid resource name %q (must match %s)", r.Name, validIdentifier.String())
	}
	for _, c := range r.Columns {
		if !validIdentifier.MatchString(c.Name) {
			return fmt.Errorf("mirror: %s: invalid column name %q", r.Name, c.Name)
		}
		t := strings.ToUpper(c.Type)
		if !allowedColumnTypes[t] {
			return fmt.Errorf("mirror: %s.%s: invalid column type %q (allowed: %v)", r.Name, c.Name, c.Type, allowedColumnTypeList())
		}
		if c.From == "" {
			return fmt.Errorf("mirror: %s.%s: From is required", r.Name, c.Name)
		}
	}

	m.resMu.Lock()
	defer m.resMu.Unlock()

	if existing, ok := m.res[r.Name]; ok {
		if err := compatibleResource(existing, r); err != nil {
			return err
		}
		// Compatible re-registration: keep the original (already migrated).
		return nil
	}

	if len(r.Columns) > 0 {
		if err := m.createTypedTable(r); err != nil {
			return err
		}
	}

	stored := r
	if len(stored.IDFields) == 0 {
		stored.IDFields = append([]string(nil), defaultIDFields...)
	} else {
		stored.IDFields = append([]string(nil), stored.IDFields...)
	}
	m.res[r.Name] = stored
	return nil
}

func compatibleResource(existing, incoming Resource) error {
	if len(existing.Columns) != len(incoming.Columns) {
		return fmt.Errorf("mirror: %s: re-registered with different column count (had %d, now %d)", incoming.Name, len(existing.Columns), len(incoming.Columns))
	}
	prev := map[string]Column{}
	for _, c := range existing.Columns {
		prev[c.Name] = c
	}
	for _, c := range incoming.Columns {
		p, ok := prev[c.Name]
		if !ok {
			return fmt.Errorf("mirror: %s: re-registered with unknown column %q", incoming.Name, c.Name)
		}
		if !strings.EqualFold(p.Type, c.Type) || p.Index != c.Index || p.From != c.From {
			return fmt.Errorf("mirror: %s.%s: re-registered with different column shape", incoming.Name, c.Name)
		}
	}
	return nil
}

func (m *Mirror) createTypedTable(r Resource) error {
	tbl := typedTableName(r.Name)
	cols := []string{`id TEXT PRIMARY KEY`, `synced_at DATETIME DEFAULT CURRENT_TIMESTAMP`}
	for _, c := range r.Columns {
		cols = append(cols, fmt.Sprintf(`"%s" %s`, c.Name, strings.ToUpper(c.Type)))
	}
	create := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s" (%s)`, tbl, strings.Join(cols, ", "))
	if _, err := m.db.Exec(create); err != nil {
		return fmt.Errorf("mirror: %s: creating typed table: %w", r.Name, err)
	}
	for _, c := range r.Columns {
		if !c.Index {
			continue
		}
		idx := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "idx_%s_%s" ON "%s"("%s")`, tbl, c.Name, tbl, c.Name)
		if _, err := m.db.Exec(idx); err != nil {
			return fmt.Errorf("mirror: %s: creating index on %s: %w", r.Name, c.Name, err)
		}
	}
	return nil
}

func typedTableName(name string) string { return "rt_" + name }

func allowedColumnTypeList() []string {
	out := make([]string, 0, len(allowedColumnTypes))
	for k := range allowedColumnTypes {
		out = append(out, k)
	}
	return out
}

func (m *Mirror) resource(name string) (Resource, bool) {
	m.resMu.RLock()
	defer m.resMu.RUnlock()
	r, ok := m.res[name]
	return r, ok
}

// Upsert inserts or updates a single item. Returns ErrUnknownResource if
// the resource family has not been registered, or ErrNoID if no
// configured IDField yields a non-empty value on the item.
func (m *Mirror) Upsert(ctx context.Context, name string, item json.RawMessage) error {
	stored, _, err := m.UpsertBatch(ctx, name, []json.RawMessage{item})
	if err != nil {
		return err
	}
	if stored == 0 {
		return ErrNoID
	}
	return nil
}

// ErrUnknownResource is returned when a method names a resource family
// that has not been Registered.
var ErrUnknownResource = fmt.Errorf("mirror: unknown resource (call Register first)")

// ErrNoID is returned when a single Upsert call cannot extract a primary
// key from the item using the resource's IDFields. UpsertBatch surfaces
// the same condition through its skipped count.
var ErrNoID = fmt.Errorf("mirror: item has no extractable id")

// UpsertBatch writes many items in a single transaction. Returns
// (stored, skipped, err) — stored is the count of rows actually landed,
// skipped counts items that failed JSON unmarshal or had no extractable
// primary key. A nil err with skipped > 0 is a partial success.
func (m *Mirror) UpsertBatch(ctx context.Context, name string, items []json.RawMessage) (int, int, error) {
	r, ok := m.resource(name)
	if !ok {
		return 0, 0, ErrUnknownResource
	}
	if len(items) == 0 {
		return 0, 0, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("mirror: begin batch: %w", err)
	}
	defer tx.Rollback()

	var stored, skipped int
	now := time.Now()
	for _, raw := range items {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			skipped++
			continue
		}
		id := extractID(obj, r.IDFields)
		if id == "" {
			skipped++
			continue
		}

		if err := upsertGenericTx(ctx, tx, r.Name, id, raw, now); err != nil {
			return stored, skipped, fmt.Errorf("mirror: %s/%s: %w", r.Name, id, err)
		}
		if len(r.Columns) > 0 {
			if err := upsertTypedTx(ctx, tx, r, id, obj, now); err != nil {
				return stored, skipped, fmt.Errorf("mirror: %s/%s typed: %w", r.Name, id, err)
			}
		}
		stored++
	}

	if err := tx.Commit(); err != nil {
		return stored, skipped, fmt.Errorf("mirror: commit batch: %w", err)
	}
	return stored, skipped, nil
}

func upsertGenericTx(ctx context.Context, tx *sql.Tx, name, id string, data json.RawMessage, now time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO resources (resource_name, id, data, synced_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(resource_name, id) DO UPDATE SET
		 data = excluded.data,
		 synced_at = excluded.synced_at,
		 updated_at = excluded.updated_at`,
		name, id, string(data), now, now,
	); err != nil {
		return err
	}

	rowid := ftsRowID(name, id)
	// FTS5 on modernc.org/sqlite is happiest with explicit rowid +
	// DELETE-by-rowid. Failure here is non-fatal — search degrades but
	// the canonical row is intact.
	if _, err := tx.ExecContext(ctx, `DELETE FROM resources_fts WHERE rowid = ?`, rowid); err != nil {
		fmt.Fprintf(os.Stderr, "mirror: warning: FTS cleanup failed for %s/%s: %v\n", name, id, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO resources_fts (rowid, id, resource_name, content) VALUES (?, ?, ?, ?)`,
		rowid, id, name, string(data),
	); err != nil {
		fmt.Fprintf(os.Stderr, "mirror: warning: FTS insert failed for %s/%s: %v\n", name, id, err)
	}
	return nil
}

func upsertTypedTx(ctx context.Context, tx *sql.Tx, r Resource, id string, obj map[string]any, now time.Time) error {
	tbl := typedTableName(r.Name)
	cols := []string{`id`, `synced_at`}
	placeholders := []string{`?`, `?`}
	updates := []string{`synced_at = excluded.synced_at`}
	args := []any{id, now}
	for _, c := range r.Columns {
		cols = append(cols, fmt.Sprintf(`"%s"`, c.Name))
		placeholders = append(placeholders, `?`)
		updates = append(updates, fmt.Sprintf(`"%s" = excluded."%s"`, c.Name, c.Name))
		args = append(args, lookupFieldValue(obj, c.From))
	}
	stmt := fmt.Sprintf(
		`INSERT INTO "%s" (%s) VALUES (%s) ON CONFLICT(id) DO UPDATE SET %s`,
		tbl, strings.Join(cols, ", "), strings.Join(placeholders, ", "), strings.Join(updates, ", "),
	)
	_, err := tx.ExecContext(ctx, stmt, args...)
	return err
}

// Get returns the stored JSON for one resource by id. Returns
// sql.ErrNoRows on miss so callers can distinguish absence via errors.Is.
func (m *Mirror) Get(ctx context.Context, name, id string) (json.RawMessage, error) {
	if _, ok := m.resource(name); !ok {
		return nil, ErrUnknownResource
	}
	var data string
	err := m.db.QueryRowContext(ctx,
		`SELECT data FROM resources WHERE resource_name = ? AND id = ?`,
		name, id,
	).Scan(&data)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// List returns up to limit items for a resource, newest-updated first.
// limit <= 0 defaults to 200.
func (m *Mirror) List(ctx context.Context, name string, limit int) ([]json.RawMessage, error) {
	if _, ok := m.resource(name); !ok {
		return nil, ErrUnknownResource
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := m.db.QueryContext(ctx,
		`SELECT data FROM resources WHERE resource_name = ?
		 ORDER BY updated_at DESC LIMIT ?`,
		name, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, rows.Err()
}

// Search runs an FTS5 MATCH against the named resource's content and
// returns up to limit results, ranked by relevance. limit <= 0 defaults
// to 50. The query is passed verbatim to FTS5 — callers control the
// dialect (prefix `*`, NEAR, OR, NOT, column filters, etc.).
func (m *Mirror) Search(ctx context.Context, name, query string, limit int) ([]json.RawMessage, error) {
	if _, ok := m.resource(name); !ok {
		return nil, ErrUnknownResource
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := m.db.QueryContext(ctx,
		`SELECT r.data
		 FROM resources r
		 JOIN resources_fts f ON r.id = f.id AND r.resource_name = f.resource_name
		 WHERE f.resource_name = ? AND resources_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		name, query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, rows.Err()
}

// SaveCursor writes the per-resource pagination cursor and stamps
// last_synced_at to now. total_count is recomputed from the resources
// table so callers don't need to track it separately.
func (m *Mirror) SaveCursor(ctx context.Context, name, cursor string) error {
	if _, ok := m.resource(name); !ok {
		return ErrUnknownResource
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var count int
	if err := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resources WHERE resource_name = ?`, name,
	).Scan(&count); err != nil {
		return fmt.Errorf("mirror: counting %s: %w", name, err)
	}

	_, err := m.db.ExecContext(ctx,
		`INSERT INTO sync_state (resource_name, last_cursor, last_synced_at, total_count)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(resource_name) DO UPDATE SET
		 last_cursor = excluded.last_cursor,
		 last_synced_at = excluded.last_synced_at,
		 total_count = excluded.total_count`,
		name, cursor, time.Now(), count,
	)
	return err
}

// GetCursor returns the last saved pagination cursor for a resource.
// Returns "" with nil error when no cursor has been saved yet.
func (m *Mirror) GetCursor(ctx context.Context, name string) (string, error) {
	if _, ok := m.resource(name); !ok {
		return "", ErrUnknownResource
	}
	var cursor sql.NullString
	err := m.db.QueryRowContext(ctx,
		`SELECT last_cursor FROM sync_state WHERE resource_name = ?`, name,
	).Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return cursor.String, nil
}

// ClearCursors deletes every sync_state row, forcing the next sync to
// re-walk from the beginning. Does not delete the resource rows
// themselves; combine with explicit DELETE FROM resources via DB() for a
// hard reset.
func (m *Mirror) ClearCursors(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.ExecContext(ctx, `DELETE FROM sync_state`)
	return err
}

// Status returns one ResourceStatus per registered resource. Unknown
// (registered but never synced) resources show Count=0 and the zero
// LastSyncedAt.
func (m *Mirror) Status(ctx context.Context) (map[string]ResourceStatus, error) {
	m.resMu.RLock()
	names := make([]string, 0, len(m.res))
	for n := range m.res {
		names = append(names, n)
	}
	m.resMu.RUnlock()

	out := make(map[string]ResourceStatus, len(names))
	for _, n := range names {
		out[n] = ResourceStatus{}
	}

	rows, err := m.db.QueryContext(ctx,
		`SELECT resource_name, COUNT(*) FROM resources GROUP BY resource_name`,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rn string
		var c int
		if err := rows.Scan(&rn, &c); err != nil {
			rows.Close()
			return nil, err
		}
		s := out[rn]
		s.Count = c
		out[rn] = s
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = m.db.QueryContext(ctx,
		`SELECT resource_name, last_synced_at, last_cursor FROM sync_state`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rn, cursor string
		var ts sql.NullTime
		if err := rows.Scan(&rn, &ts, &cursor); err != nil {
			return nil, err
		}
		s := out[rn]
		if ts.Valid {
			s.LastSyncedAt = ts.Time
		}
		s.LastCursor = cursor
		out[rn] = s
	}
	return out, rows.Err()
}

// extractID walks the configured fallback list and returns the first
// non-empty value rendered as a string. Mirrors the petstore reference's
// LookupFieldValue snake_case-then-camelCase convention.
func extractID(obj map[string]any, fields []string) string {
	tries := fields
	if len(tries) == 0 {
		tries = defaultIDFields
	}
	for _, key := range tries {
		v := lookupFieldValue(obj, key)
		if v == nil {
			continue
		}
		s := fmt.Sprintf("%v", v)
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

// lookupFieldValue resolves a field value from a JSON object, trying the
// snake_case key first and the camelCase rendering second. Non-scalar
// values (objects, arrays) are JSON-marshaled into a string so they can
// be stored in a TEXT column without driver fuss.
func lookupFieldValue(obj map[string]any, snakeKey string) any {
	if v, ok := obj[snakeKey]; ok {
		return scalarize(v)
	}
	parts := strings.Split(snakeKey, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	if v, ok := obj[strings.Join(parts, "")]; ok {
		return scalarize(v)
	}
	return nil
}

func scalarize(v any) any {
	switch v.(type) {
	case nil, string, bool, int, int64, float64, []byte:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

// ftsRowID derives a deterministic positive int64 rowid from
// (resourceName, id). Collisions across different (name, id) pairs are
// possible but cosmetic — the canonical resources row is keyed
// independently.
func ftsRowID(scope, id string) int64 {
	var h uint64
	for _, c := range scope {
		h = h*31 + uint64(c)
	}
	h *= 31
	for _, c := range id {
		h = h*31 + uint64(c)
	}
	return int64(h & 0x7FFFFFFFFFFFFFFF)
}
