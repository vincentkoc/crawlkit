package pack

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/vincentkoc/crawlkit/sqlitekit"
)

func TestExportImportTablesWithFilter(t *testing.T) {
	ctx := context.Background()
	src, err := sqlitekit.Open(ctx, sqlitekit.Options{
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

	dst, err := sqlitekit.Open(ctx, sqlitekit.Options{
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
	src, err := sqlitekit.Open(ctx, sqlitekit.Options{
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

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}
