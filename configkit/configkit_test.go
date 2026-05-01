package configkit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathsUseConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := (App{Name: "thingcrawl"}).DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	wantBase := filepath.Join(home, ".config", "thingcrawl")
	if paths.BaseDir != wantBase {
		t.Fatalf("base dir = %q, want %q", paths.BaseDir, wantBase)
	}
	if paths.ConfigPath != filepath.Join(wantBase, "config.toml") {
		t.Fatalf("config path = %q", paths.ConfigPath)
	}
}

func TestResolveConfigPathPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("THINGCRAWL_CONFIG", "~/custom/config.toml")

	app := App{Name: "thingcrawl"}
	path, err := app.ResolveConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, "custom", "config.toml") {
		t.Fatalf("env path = %q", path)
	}

	path, err = app.ResolveConfigPath("~/flag/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, "flag", "config.toml") {
		t.Fatalf("flag path = %q", path)
	}
}

func TestRuntimeConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := RuntimeConfig{Version: 1, DBPath: filepath.Join(dir, "x.db")}
	if err := WriteTOML(path, cfg, 0); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o", got)
	}
	var loaded RuntimeConfig
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.DBPath != cfg.DBPath {
		t.Fatalf("loaded db path = %q", loaded.DBPath)
	}
}

func TestTokenDiagnosticDoesNotExposeValue(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "super-secret")
	got := TokenDiagnosticForEnv("SECRET_TOKEN")
	if !got.Present || got.Source != "env" {
		t.Fatalf("diagnostic = %+v", got)
	}
}
