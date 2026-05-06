package state

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSetGetAndStale(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(db, func() time.Time { return now })
	if err := store.Set(ctx, "share", "repo", "last_import", "2026-05-01T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := store.Get(ctx, "share", "repo", "last_import")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rec.Value == "" {
		t.Fatalf("record not found: %+v", rec)
	}
	stale, err := store.IsStale(ctx, "share", "repo", "last_import", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("fresh record reported stale")
	}
	store.now = func() time.Time { return now.Add(2 * time.Hour) }
	stale, err = store.IsStale(ctx, "share", "repo", "last_import", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("old record reported fresh")
	}
}

func TestScopedStoreSetGetAndStale(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "scoped.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureScopedSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := NewScopedWithClock(db, func() time.Time { return now })
	if err := store.Set(ctx, "share:last_import_at", "2026-05-01T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := store.Get(ctx, "share:last_import_at")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rec.Cursor == "" {
		t.Fatalf("record not found: %+v", rec)
	}
	stale, err := store.IsStale(ctx, "share:last_import_at", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("fresh scoped record reported stale")
	}
	store.now = func() time.Time { return now.Add(2 * time.Hour) }
	stale, err = store.IsStale(ctx, "share:last_import_at", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("old scoped record reported fresh")
	}
}

func TestCursorStoreSetGetAndStale(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "cursor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureCursorSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := NewCursorWithClock(db, func() time.Time { return now })
	if err := store.Set(ctx, "share", "manifest", "generated_at", "2026-05-01T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := store.Get(ctx, "share", "manifest", "generated_at")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rec.Cursor == "" {
		t.Fatalf("record not found: %+v", rec)
	}
	stale, err := store.IsStale(ctx, "share", "manifest", "generated_at", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("fresh cursor record reported stale")
	}
	store.now = func() time.Time { return now.Add(2 * time.Hour) }
	stale, err = store.IsStale(ctx, "share", "manifest", "generated_at", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("old cursor record reported fresh")
	}
}
