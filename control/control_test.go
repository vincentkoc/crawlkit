package control

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestDefaultsSchemaAndBinary(t *testing.T) {
	manifest := NewManifest("slacrawl", "Slack Crawl", "slacrawl")
	if manifest.SchemaVersion != SchemaVersion {
		t.Fatalf("schema = %q", manifest.SchemaVersion)
	}
	if manifest.Binary.Name != "slacrawl" {
		t.Fatalf("binary = %#v", manifest.Binary)
	}
	if manifest.Commands == nil {
		t.Fatal("commands map should be initialized")
	}
}

func TestSQLiteDatabaseStatsPathReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.db")
	if err := os.WriteFile(path, []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := SQLiteDatabase("primary", "Primary archive", "archive", path, true, []Count{NewCount("messages", "Messages", 7)})
	if db.Kind != "sqlite" || !db.IsPrimary || db.Bytes != 6 {
		t.Fatalf("unexpected database: %#v", db)
	}
	if db.ModifiedAt == "" {
		t.Fatal("modified_at should be set for existing paths")
	}
	if len(db.Counts) != 1 || db.Counts[0].Value != 7 {
		t.Fatalf("counts = %#v", db.Counts)
	}
}
