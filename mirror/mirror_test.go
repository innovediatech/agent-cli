package mirror_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/innovediatech/agent-cli/mirror"
)

func newMirror(t *testing.T) *mirror.Mirror {
	t.Helper()
	m, err := mirror.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func mustRegister(t *testing.T, m *mirror.Mirror, r mirror.Resource) {
	t.Helper()
	if err := m.Register(r); err != nil {
		t.Fatalf("register %s: %v", r.Name, err)
	}
}

func mustUpsert(t *testing.T, m *mirror.Mirror, name string, item any) {
	t.Helper()
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := m.Upsert(context.Background(), name, raw); err != nil {
		t.Fatalf("upsert %s: %v", name, err)
	}
}

func TestOpenAndClose(t *testing.T) {
	m, err := mirror.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if m.Path() != ":memory:" {
		t.Errorf("path = %q, want :memory:", m.Path())
	}
	if err := m.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "subdir")
	dbPath := filepath.Join(dir, "mirror.db")
	m, err := mirror.Open(dbPath)
	if err != nil {
		t.Fatalf("open in nested dir: %v", err)
	}
	defer m.Close()
	if m.Path() != dbPath {
		t.Errorf("path mismatch: %s vs %s", m.Path(), dbPath)
	}
}

func TestRegisterValidation(t *testing.T) {
	m := newMirror(t)
	tests := []struct {
		name    string
		res     mirror.Resource
		wantErr string
	}{
		{"empty name", mirror.Resource{Name: ""}, "invalid resource name"},
		{"name with space", mirror.Resource{Name: "my contacts"}, "invalid resource name"},
		{"name starts with digit", mirror.Resource{Name: "1contacts"}, "invalid resource name"},
		{"bad column type", mirror.Resource{Name: "x", Columns: []mirror.Column{{Name: "c", Type: "STRING", From: "f"}}}, "invalid column type"},
		{"bad column name", mirror.Resource{Name: "x", Columns: []mirror.Column{{Name: "c d", Type: "TEXT", From: "f"}}}, "invalid column name"},
		{"missing From", mirror.Resource{Name: "x", Columns: []mirror.Column{{Name: "c", Type: "TEXT"}}}, "From is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := m.Register(tc.res)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestRegisterIdempotent(t *testing.T) {
	m := newMirror(t)
	r := mirror.Resource{
		Name: "contacts",
		Columns: []mirror.Column{
			{Name: "email", Type: "TEXT", From: "email", Index: true},
		},
	}
	mustRegister(t, m, r)
	if err := m.Register(r); err != nil {
		t.Fatalf("re-register identical: %v", err)
	}
}

func TestRegisterRejectsIncompatibleReregistration(t *testing.T) {
	m := newMirror(t)
	r := mirror.Resource{
		Name: "contacts",
		Columns: []mirror.Column{
			{Name: "email", Type: "TEXT", From: "email"},
		},
	}
	mustRegister(t, m, r)
	r2 := mirror.Resource{
		Name: "contacts",
		Columns: []mirror.Column{
			{Name: "email", Type: "INTEGER", From: "email"},
		},
	}
	if err := m.Register(r2); err == nil {
		t.Fatal("want error for type change, got nil")
	}
	r3 := mirror.Resource{Name: "contacts"}
	if err := m.Register(r3); err == nil {
		t.Fatal("want error for column-count change, got nil")
	}
}

func TestUpsertAndGet(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})
	mustUpsert(t, m, "deals", map[string]any{
		"id":     "deal-1",
		"title":  "First deal",
		"amount": 1500,
	})

	raw, err := m.Get(context.Background(), "deals", "deal-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["title"] != "First deal" {
		t.Errorf("title = %v, want First deal", got["title"])
	}
}

func TestGetReturnsErrNoRowsOnMiss(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})
	_, err := m.Get(context.Background(), "deals", "nope")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestUnknownResource(t *testing.T) {
	m := newMirror(t)
	_, err := m.Get(context.Background(), "ghost", "x")
	if !errors.Is(err, mirror.ErrUnknownResource) {
		t.Errorf("Get on unregistered: err = %v, want ErrUnknownResource", err)
	}
	raw, _ := json.Marshal(map[string]any{"id": "x"})
	if err := m.Upsert(context.Background(), "ghost", raw); !errors.Is(err, mirror.ErrUnknownResource) {
		t.Errorf("Upsert on unregistered: err = %v, want ErrUnknownResource", err)
	}
}

func TestUpsertReplacesExistingRow(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})
	mustUpsert(t, m, "deals", map[string]any{"id": "d1", "stage": "lead"})
	mustUpsert(t, m, "deals", map[string]any{"id": "d1", "stage": "won"})

	raw, err := m.Get(context.Background(), "deals", "d1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(string(raw), `"stage":"won"`) {
		t.Errorf("data = %s, want stage=won", string(raw))
	}
}

func TestUpsertNoIDReturnsErrNoID(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})
	raw, _ := json.Marshal(map[string]any{"title": "no id"})
	err := m.Upsert(context.Background(), "deals", raw)
	if !errors.Is(err, mirror.ErrNoID) {
		t.Errorf("err = %v, want ErrNoID", err)
	}
}

func TestUpsertCustomIDFields(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{
		Name:     "users",
		IDFields: []string{"email"},
	})
	mustUpsert(t, m, "users", map[string]any{"email": "j@x.com", "name": "Jay"})
	raw, err := m.Get(context.Background(), "users", "j@x.com")
	if err != nil {
		t.Fatalf("get by email-as-id: %v", err)
	}
	if !strings.Contains(string(raw), `"name":"Jay"`) {
		t.Errorf("got %s", string(raw))
	}
}

func TestCamelCaseFallback(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{
		Name:     "users",
		IDFields: []string{"user_id"},
	})
	// camelCase incoming, snake_case in IDFields
	mustUpsert(t, m, "users", map[string]any{"userId": "u-1", "name": "A"})
	if _, err := m.Get(context.Background(), "users", "u-1"); err != nil {
		t.Errorf("camelCase fallback get: %v", err)
	}
}

func TestUpsertBatchPartialSuccess(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})

	good, _ := json.Marshal(map[string]any{"id": "d1", "title": "ok"})
	bad := json.RawMessage(`{not json`)
	noID, _ := json.Marshal(map[string]any{"title": "no id"})

	stored, skipped, err := m.UpsertBatch(context.Background(), "deals",
		[]json.RawMessage{good, bad, noID, good})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if stored != 2 {
		t.Errorf("stored = %d, want 2", stored)
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
}

func TestList(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})
	for i, title := range []string{"alpha", "bravo", "charlie"} {
		mustUpsert(t, m, "deals", map[string]any{
			"id":    string(rune('a' + i)),
			"title": title,
		})
	}
	out, err := m.List(context.Background(), "deals", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("len = %d, want 3", len(out))
	}
}

func TestSearchFTS5(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "notes"})
	mustUpsert(t, m, "notes", map[string]any{"id": "n1", "body": "the quick brown fox"})
	mustUpsert(t, m, "notes", map[string]any{"id": "n2", "body": "lazy dog asleep"})
	mustUpsert(t, m, "notes", map[string]any{"id": "n3", "body": "fox and hound"})

	hits, err := m.Search(context.Background(), "notes", "fox", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("hits for 'fox' = %d, want 2", len(hits))
	}

	noHits, err := m.Search(context.Background(), "notes", "elephant", 10)
	if err != nil {
		t.Fatalf("search empty: %v", err)
	}
	if len(noHits) != 0 {
		t.Errorf("hits for 'elephant' = %d, want 0", len(noHits))
	}
}

func TestSearchScopedByResource(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "notes"})
	mustRegister(t, m, mirror.Resource{Name: "tasks"})
	mustUpsert(t, m, "notes", map[string]any{"id": "n1", "body": "find the fox"})
	mustUpsert(t, m, "tasks", map[string]any{"id": "t1", "body": "find the fox"})

	got, err := m.Search(context.Background(), "notes", "fox", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("notes-scoped hits = %d, want 1", len(got))
	}
}

func TestTypedColumnsAndDirectQuery(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{
		Name: "contacts",
		Columns: []mirror.Column{
			{Name: "email", Type: "TEXT", From: "email", Index: true},
			{Name: "score", Type: "INTEGER", From: "score"},
		},
	})
	mustUpsert(t, m, "contacts", map[string]any{"id": "c1", "email": "a@x.com", "score": 80})
	mustUpsert(t, m, "contacts", map[string]any{"id": "c2", "email": "b@x.com", "score": 40})
	mustUpsert(t, m, "contacts", map[string]any{"id": "c3", "email": "c@x.com", "score": 95})

	rows, err := m.DB().Query(`SELECT id, email FROM rt_contacts WHERE score >= ? ORDER BY score DESC`, 80)
	if err != nil {
		t.Fatalf("typed query: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id+":"+email)
	}
	if len(ids) != 2 {
		t.Errorf("got %v, want 2 results", ids)
	}
	if ids[0] != "c3:c@x.com" {
		t.Errorf("first = %s, want c3:c@x.com", ids[0])
	}
}

func TestTypedColumnUpsertOverwrites(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{
		Name: "contacts",
		Columns: []mirror.Column{
			{Name: "email", Type: "TEXT", From: "email"},
		},
	})
	mustUpsert(t, m, "contacts", map[string]any{"id": "c1", "email": "old@x.com"})
	mustUpsert(t, m, "contacts", map[string]any{"id": "c1", "email": "new@x.com"})

	var email string
	if err := m.DB().QueryRow(`SELECT email FROM rt_contacts WHERE id = ?`, "c1").Scan(&email); err != nil {
		t.Fatalf("query: %v", err)
	}
	if email != "new@x.com" {
		t.Errorf("email = %q, want new@x.com", email)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})

	got, err := m.GetCursor(context.Background(), "deals")
	if err != nil {
		t.Fatalf("get empty cursor: %v", err)
	}
	if got != "" {
		t.Errorf("empty cursor = %q, want empty", got)
	}

	if err := m.SaveCursor(context.Background(), "deals", "page=2"); err != nil {
		t.Fatalf("save cursor: %v", err)
	}
	got, err = m.GetCursor(context.Background(), "deals")
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if got != "page=2" {
		t.Errorf("cursor = %q, want page=2", got)
	}
}

func TestClearCursors(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})
	if err := m.SaveCursor(context.Background(), "deals", "x"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := m.ClearCursors(context.Background()); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, err := m.GetCursor(context.Background(), "deals")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "" {
		t.Errorf("after clear cursor = %q, want empty", got)
	}
}

func TestStatus(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})
	mustRegister(t, m, mirror.Resource{Name: "notes"})

	for i := 0; i < 3; i++ {
		mustUpsert(t, m, "deals", map[string]any{"id": string(rune('a' + i))})
	}
	if err := m.SaveCursor(context.Background(), "deals", "cursor-here"); err != nil {
		t.Fatalf("save cursor: %v", err)
	}

	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st["deals"].Count != 3 {
		t.Errorf("deals count = %d, want 3", st["deals"].Count)
	}
	if st["deals"].LastCursor != "cursor-here" {
		t.Errorf("deals cursor = %q", st["deals"].LastCursor)
	}
	if st["deals"].LastSyncedAt.IsZero() {
		t.Error("deals LastSyncedAt zero, want set")
	}
	if _, ok := st["notes"]; !ok {
		t.Error("notes missing from status (registered, never synced)")
	}
	if st["notes"].Count != 0 {
		t.Errorf("notes count = %d, want 0", st["notes"].Count)
	}
}

func TestPersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "m.db")

	m1, err := mirror.Open(dbPath)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	mustRegister(t, m1, mirror.Resource{
		Name: "contacts",
		Columns: []mirror.Column{
			{Name: "email", Type: "TEXT", From: "email"},
		},
	})
	mustUpsert(t, m1, "contacts", map[string]any{"id": "c1", "email": "x@y.com"})
	if err := m1.SaveCursor(context.Background(), "contacts", "after-c1"); err != nil {
		t.Fatalf("save cursor: %v", err)
	}
	if err := m1.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}

	m2, err := mirror.Open(dbPath)
	if err != nil {
		t.Fatalf("open 2: %v", err)
	}
	defer m2.Close()
	mustRegister(t, m2, mirror.Resource{
		Name: "contacts",
		Columns: []mirror.Column{
			{Name: "email", Type: "TEXT", From: "email"},
		},
	})

	raw, err := m2.Get(context.Background(), "contacts", "c1")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if !strings.Contains(string(raw), `"email":"x@y.com"`) {
		t.Errorf("data lost: %s", string(raw))
	}
	cur, err := m2.GetCursor(context.Background(), "contacts")
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if cur != "after-c1" {
		t.Errorf("cursor lost: %q", cur)
	}
}

func TestConcurrentWritesSerialize(t *testing.T) {
	m := newMirror(t)
	mustRegister(t, m, mirror.Resource{Name: "deals"})

	const N = 50
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			raw, _ := json.Marshal(map[string]any{
				"id":    "d-" + string(rune('a'+(i%26))) + "-" + string(rune('0'+(i/26))),
				"title": "concurrent",
			})
			errs <- m.Upsert(context.Background(), "deals", raw)
		}(i)
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent upsert %d: %v", i, err)
		}
	}
}
