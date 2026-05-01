package mirror

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRepoCommitDirty(t *testing.T) {
	ctx := context.Background()
	repo := filepath.Join(t.TempDir(), "share")
	opts := Options{RepoPath: repo, Branch: "main"}
	if err := EnsureRepo(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		t.Fatal(err)
	}
	dirty, err := Dirty(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Fatal("new repo should be clean")
	}
	if err := os.WriteFile(filepath.Join(repo, "manifest.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	committed, err := Commit(ctx, opts, "archive: test")
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected commit")
	}
	dirty, err = Dirty(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Fatal("repo should be clean after commit")
	}
}
