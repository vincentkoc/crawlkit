package snapshot

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vincentkoc/crawlkit/store"
)

func TestExportImportTablesWithFilter(t *testing.T) {
	ctx := context.Background()
	src, err := store.Open(ctx, store.Options{
		Path: filepath.Join(t.TempDir(), "src.db"),
		Schema: `
create table messages(id text primary key, guild_id text not null, body text not null);
create table sync_state(source_name text, entity_type text, entity_id text, value text, updated_at text, primary key(source_name, entity_type, entity_id));
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	mustExec(t, src.DB(), `insert into messages(id, guild_id, body) values('1', 'guild', 'hello')`)
	mustExec(t, src.DB(), `insert into messages(id, guild_id, body) values('2', '@me', 'private')`)
	mustExec(t, src.DB(), `insert into sync_state(source_name, entity_type, entity_id, value, updated_at) values('api', 'cursor', 'x', '1', 'now')`)

	root := t.TempDir()
	manifest, err := Export(ctx, ExportOptions{
		DB:      src.DB(),
		RootDir: root,
		Tables:  []string{"messages", "sync_state"},
		Now:     func() time.Time { return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC) },
		Filter: func(table string, row map[string]any) (bool, error) {
			return !(table == "messages" && row["guild_id"] == "@me"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Tables) != 2 || manifest.Tables[0].Rows != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}

	dst, err := store.Open(ctx, store.Options{
		Path: filepath.Join(t.TempDir(), "dst.db"),
		Schema: `
create table messages(id text primary key, guild_id text not null, body text not null);
create table sync_state(source_name text, entity_type text, entity_id text, value text, updated_at text, primary key(source_name, entity_type, entity_id));
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if _, err := Import(ctx, ImportOptions{DB: dst.DB(), RootDir: root}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dst.DB().QueryRowContext(ctx, `select count(*) from messages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("message count = %d", count)
	}
}

func TestExportRotatesShards(t *testing.T) {
	ctx := context.Background()
	src, err := store.Open(ctx, store.Options{
		Path:   filepath.Join(t.TempDir(), "src.db"),
		Schema: `create table things(id integer primary key, value text not null);`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	for i := 0; i < 25; i++ {
		mustExec(t, src.DB(), `insert into things(value) values('some repeated text to force shard rotation')`)
	}
	manifest, err := Export(ctx, ExportOptions{
		DB:            src.DB(),
		RootDir:       t.TempDir(),
		Tables:        []string{"things"},
		MaxShardBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Tables[0].Files) < 2 {
		t.Fatalf("expected multiple shards, got %+v", manifest.Tables[0].Files)
	}
}

func TestImportHooks(t *testing.T) {
	ctx := context.Background()
	src, err := store.Open(ctx, store.Options{
		Path:   filepath.Join(t.TempDir(), "src.db"),
		Schema: `create table things(id text primary key, keep integer not null);`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	mustExec(t, src.DB(), `insert into things(id, keep) values('new', 1)`)
	root := t.TempDir()
	if _, err := Export(ctx, ExportOptions{DB: src.DB(), RootDir: root, Tables: []string{"things"}}); err != nil {
		t.Fatal(err)
	}

	dst, err := store.Open(ctx, store.Options{
		Path:   filepath.Join(t.TempDir(), "dst.db"),
		Schema: `create table things(id text primary key, keep integer not null); create table audit(event text not null);`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	mustExec(t, dst.DB(), `insert into things(id, keep) values('local', 0)`)
	if _, err := Import(ctx, ImportOptions{
		DB:      dst.DB(),
		RootDir: root,
		BeforeImport: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `insert into audit(event) values('before')`)
			return err
		},
		DeleteTable: func(ctx context.Context, tx *sql.Tx, table string) error {
			_, err := tx.ExecContext(ctx, `delete from `+store.QuoteIdent(table)+` where keep != 0`)
			return err
		},
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dst.DB().QueryRowContext(ctx, `select count(*) from things where id = 'local'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("custom delete hook removed local row")
	}
}

func TestImportLegacySingularFileManifest(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rel := filepath.ToSlash(filepath.Join("tables", "things", "legacy.jsonl.gz"))
	writeGzipJSONL(t, filepath.Join(root, filepath.FromSlash(rel)), map[string]any{"id": "new", "body": "from legacy"})
	if err := WriteManifest(root, Manifest{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC),
		Tables: []TableManifest{{
			Name:    "things",
			File:    rel,
			Columns: []string{"id", "body"},
			Rows:    1,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	dst, err := store.Open(ctx, store.Options{
		Path:   filepath.Join(t.TempDir(), "dst.db"),
		Schema: `create table things(id text primary key, body text not null);`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	mustExec(t, dst.DB(), `insert into things(id, body) values('old', 'before')`)
	if _, err := Import(ctx, ImportOptions{DB: dst.DB(), RootDir: root}); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := dst.DB().QueryRowContext(ctx, `select group_concat(id || ':' || body, ',') from things`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "new:from legacy" {
		t.Fatalf("things = %q", got)
	}
}

func TestImportFilterSkipsRows(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rel := filepath.ToSlash(filepath.Join("tables", "messages", "000000.jsonl.gz"))
	writeGzipJSONL(t,
		filepath.Join(root, filepath.FromSlash(rel)),
		map[string]any{"id": "public", "guild_id": "g1", "body": "keep"},
		map[string]any{"id": "dm", "guild_id": "@me", "body": "skip"},
	)
	if err := WriteManifest(root, Manifest{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 2, 9, 5, 0, 0, time.UTC),
		Tables: []TableManifest{{
			Name:    "messages",
			Files:   []string{rel},
			Columns: []string{"id", "guild_id", "body"},
			Rows:    2,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	dst, err := store.Open(ctx, store.Options{
		Path:   filepath.Join(t.TempDir(), "dst.db"),
		Schema: `create table messages(id text primary key, guild_id text not null, body text not null);`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if _, err := Import(ctx, ImportOptions{
		DB:      dst.DB(),
		RootDir: root,
		Filter: func(table string, row map[string]any) (bool, error) {
			return !(table == "messages" && row["guild_id"] == "@me"), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dst.DB().QueryRowContext(ctx, `select count(*) from messages where guild_id = '@me'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("private rows imported = %d", count)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func writeGzipJSONL(t *testing.T, path string, rows ...map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	enc := json.NewEncoder(gz)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
