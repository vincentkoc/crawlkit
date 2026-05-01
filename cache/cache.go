package cache

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type SnapshotOptions struct {
	SourcePath   string
	CacheDir     string
	Name         string
	MaxFileBytes int64
	Now          func() time.Time
}

type Snapshot struct {
	SourcePath string    `json:"source_path"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

func SnapshotFile(opts SnapshotOptions) (Snapshot, error) {
	if opts.SourcePath == "" {
		return Snapshot{}, errors.New("source path is required")
	}
	if opts.CacheDir == "" {
		return Snapshot{}, errors.New("cache dir is required")
	}
	info, err := os.Stat(opts.SourcePath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stat source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("source is not a regular file: %s", opts.SourcePath)
	}
	if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
		return Snapshot{}, fmt.Errorf("source file is %d bytes, exceeds limit %d", info.Size(), opts.MaxFileBytes)
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("create cache dir: %w", err)
	}
	name := opts.Name
	if name == "" {
		name = filepath.Base(opts.SourcePath)
	}
	target := filepath.Join(opts.CacheDir, now().UTC().Format("20060102T150405Z")+"-"+name)
	src, err := os.Open(opts.SourcePath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open source: %w", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Snapshot{}, fmt.Errorf("create snapshot: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return Snapshot{}, fmt.Errorf("copy snapshot: %w", err)
	}
	if err := dst.Close(); err != nil {
		return Snapshot{}, fmt.Errorf("close snapshot: %w", err)
	}
	return Snapshot{SourcePath: opts.SourcePath, Path: target, SizeBytes: info.Size(), CreatedAt: now().UTC()}, nil
}
