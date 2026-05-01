package configkit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type App struct {
	Name          string
	ConfigEnv     string
	BaseDir       string
	LegacyBaseDir string
}

type Paths struct {
	BaseDir    string `toml:"base_dir" json:"base_dir"`
	ConfigPath string `toml:"config_path" json:"config_path"`
	DBPath     string `toml:"db_path" json:"db_path"`
	CacheDir   string `toml:"cache_dir" json:"cache_dir"`
	LogDir     string `toml:"log_dir" json:"log_dir"`
	ShareDir   string `toml:"share_dir" json:"share_dir"`
}

type RuntimeConfig struct {
	Version  int    `toml:"version" json:"version"`
	DBPath   string `toml:"db_path" json:"db_path"`
	CacheDir string `toml:"cache_dir" json:"cache_dir"`
	LogDir   string `toml:"log_dir" json:"log_dir"`
	ShareDir string `toml:"share_dir" json:"share_dir"`
}

type TokenDiagnostic struct {
	Env     string `json:"env"`
	Present bool   `json:"present"`
	Source  string `json:"source,omitempty"`
}

func (a App) Normalize() (App, error) {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return App{}, errors.New("app name is required")
	}
	if a.ConfigEnv == "" {
		a.ConfigEnv = strings.ToUpper(strings.ReplaceAll(a.Name, "-", "_")) + "_CONFIG"
	}
	if a.BaseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return App{}, err
		}
		a.BaseDir = filepath.Join(home, ".config", a.Name)
	}
	return a, nil
}

func (a App) DefaultPaths() (Paths, error) {
	app, err := a.Normalize()
	if err != nil {
		return Paths{}, err
	}
	base := ExpandHome(app.BaseDir)
	return Paths{
		BaseDir:    base,
		ConfigPath: filepath.Join(base, "config.toml"),
		DBPath:     filepath.Join(base, app.Name+".db"),
		CacheDir:   filepath.Join(base, "cache"),
		LogDir:     filepath.Join(base, "logs"),
		ShareDir:   filepath.Join(base, "share"),
	}, nil
}

func (a App) LegacyPaths() (Paths, bool, error) {
	app, err := a.Normalize()
	if err != nil {
		return Paths{}, false, err
	}
	if strings.TrimSpace(app.LegacyBaseDir) == "" {
		return Paths{}, false, nil
	}
	base := ExpandHome(app.LegacyBaseDir)
	return Paths{
		BaseDir:    base,
		ConfigPath: filepath.Join(base, "config.toml"),
		DBPath:     filepath.Join(base, app.Name+".db"),
		CacheDir:   filepath.Join(base, "cache"),
		LogDir:     filepath.Join(base, "logs"),
		ShareDir:   filepath.Join(base, "share"),
	}, true, nil
}

func (a App) ResolveConfigPath(flagPath string) (string, error) {
	app, err := a.Normalize()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(flagPath) != "" {
		return ExpandHome(flagPath), nil
	}
	if envPath := strings.TrimSpace(os.Getenv(app.ConfigEnv)); envPath != "" {
		return ExpandHome(envPath), nil
	}
	paths, err := app.DefaultPaths()
	if err != nil {
		return "", err
	}
	return paths.ConfigPath, nil
}

func (a App) DefaultRuntimeConfig() (RuntimeConfig, error) {
	paths, err := a.DefaultPaths()
	if err != nil {
		return RuntimeConfig{}, err
	}
	return RuntimeConfig{
		Version:  1,
		DBPath:   paths.DBPath,
		CacheDir: paths.CacheDir,
		LogDir:   paths.LogDir,
		ShareDir: paths.ShareDir,
	}, nil
}

func ApplyRuntimeDefaults(cfg *RuntimeConfig, defaults RuntimeConfig) {
	if cfg.Version == 0 {
		cfg.Version = defaults.Version
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaults.DBPath
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = defaults.CacheDir
	}
	if cfg.LogDir == "" {
		cfg.LogDir = defaults.LogDir
	}
	if cfg.ShareDir == "" {
		cfg.ShareDir = defaults.ShareDir
	}
	cfg.DBPath = ExpandHome(cfg.DBPath)
	cfg.CacheDir = ExpandHome(cfg.CacheDir)
	cfg.LogDir = ExpandHome(cfg.LogDir)
	cfg.ShareDir = ExpandHome(cfg.ShareDir)
}

func EnsureRuntimeDirs(cfg RuntimeConfig) error {
	for _, path := range []string{filepath.Dir(cfg.DBPath), cfg.CacheDir, cfg.LogDir, cfg.ShareDir} {
		if strings.TrimSpace(path) == "" || path == "." {
			continue
		}
		if err := os.MkdirAll(ExpandHome(path), 0o755); err != nil {
			return fmt.Errorf("create runtime dir %s: %w", path, err)
		}
	}
	return nil
}

func LoadTOML(path string, dst any) error {
	data, err := os.ReadFile(ExpandHome(path))
	if err != nil {
		return err
	}
	if err := toml.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("parse toml %s: %w", path, err)
	}
	return nil
}

func WriteTOML(path string, src any, perm os.FileMode) error {
	resolved := ExpandHome(path)
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := toml.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshal toml: %w", err)
	}
	if perm == 0 {
		perm = 0o600
	}
	return os.WriteFile(resolved, data, perm)
}

func TokenDiagnosticForEnv(env string) TokenDiagnostic {
	env = strings.TrimSpace(env)
	if env == "" {
		return TokenDiagnostic{}
	}
	_, present := os.LookupEnv(env)
	source := ""
	if present {
		source = "env"
	}
	return TokenDiagnostic{Env: env, Present: present, Source: source}
}

func ExpandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
