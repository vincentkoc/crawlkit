package snapshot

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vincentkoc/crawlkit/store"
)

const ManifestName = "manifest.json"

const defaultMaxShardBytes int64 = 40 * 1024 * 1024

type ExportOptions struct {
	DB            *sql.DB
	RootDir       string
	Tables        []string
	MaxShardBytes int64
	Filter        RowFilter
	Sidecars      []Sidecar
	Now           func() time.Time
}

type ImportOptions struct {
	DB           *sql.DB
	RootDir      string
	DeleteTables []string
	DeleteTable  DeleteFunc
	BeforeImport func(context.Context, *sql.Tx) error
	AfterImport  func(context.Context, *sql.Tx) error
}

type RowFilter func(table string, row map[string]any) (bool, error)

type DeleteFunc func(ctx context.Context, tx *sql.Tx, table string) error

type Sidecar struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
}

type Manifest struct {
	Version     int               `json:"version"`
	GeneratedAt time.Time         `json:"generated_at"`
	Tables      []TableManifest   `json:"tables"`
	Sidecars    []Sidecar         `json:"sidecars,omitempty"`
	Files       map[string]string `json:"files,omitempty"`
}

type TableManifest struct {
	Name    string   `json:"name"`
	File    string   `json:"file,omitempty"`
	Files   []string `json:"files"`
	Columns []string `json:"columns"`
	Rows    int      `json:"rows"`
}

var ErrNoManifest = errors.New("pack manifest not found")

func Export(ctx context.Context, opts ExportOptions) (Manifest, error) {
	if opts.DB == nil {
		return Manifest{}, errors.New("db is required")
	}
	if strings.TrimSpace(opts.RootDir) == "" {
		return Manifest{}, errors.New("root dir is required")
	}
	if len(opts.Tables) == 0 {
		return Manifest{}, errors.New("at least one table is required")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	maxShardBytes := opts.MaxShardBytes
	if maxShardBytes == 0 {
		maxShardBytes = defaultMaxShardBytes
	}
	tablesDir := filepath.Join(opts.RootDir, "tables")
	if err := os.RemoveAll(tablesDir); err != nil {
		return Manifest{}, fmt.Errorf("reset tables dir: %w", err)
	}
	if err := os.MkdirAll(tablesDir, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("create tables dir: %w", err)
	}
	manifest := Manifest{
		Version:     1,
		GeneratedAt: now().UTC(),
		Sidecars:    opts.Sidecars,
		Files:       map[string]string{"manifest": ManifestName},
	}
	for _, table := range opts.Tables {
		entry, err := exportTable(ctx, opts.DB, opts.RootDir, table, maxShardBytes, opts.Filter)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Tables = append(manifest.Tables, entry)
	}
	if err := WriteManifest(opts.RootDir, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func Import(ctx context.Context, opts ImportOptions) (Manifest, error) {
	if opts.DB == nil {
		return Manifest{}, errors.New("db is required")
	}
	manifest, err := ReadManifest(opts.RootDir)
	if err != nil {
		return Manifest{}, err
	}
	deleteTables := opts.DeleteTables
	if len(deleteTables) == 0 {
		for _, table := range manifest.Tables {
			deleteTables = append(deleteTables, table.Name)
		}
	}
	tx, err := opts.DB.BeginTx(ctx, nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("begin import tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if opts.BeforeImport != nil {
		if err := opts.BeforeImport(ctx, tx); err != nil {
			return Manifest{}, err
		}
	}
	for i := len(deleteTables) - 1; i >= 0; i-- {
		table := strings.TrimSpace(deleteTables[i])
		if table == "" {
			continue
		}
		if opts.DeleteTable != nil {
			if err := opts.DeleteTable(ctx, tx, table); err != nil {
				return Manifest{}, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, "delete from "+store.QuoteIdent(table)); err != nil {
			return Manifest{}, fmt.Errorf("clear table %s: %w", table, err)
		}
	}
	for _, table := range manifest.Tables {
		if err := importTable(ctx, tx, opts.RootDir, table); err != nil {
			return Manifest{}, err
		}
	}
	if opts.AfterImport != nil {
		if err := opts.AfterImport(ctx, tx); err != nil {
			return Manifest{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Manifest{}, fmt.Errorf("commit import tx: %w", err)
	}
	committed = true
	return manifest, nil
}

func ReadManifest(rootDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(rootDir, ManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, ErrNoManifest
	}
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest, nil
}

func WriteManifest(rootDir string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, ManifestName), data, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func exportTable(ctx context.Context, db *sql.DB, rootDir, table string, maxShardBytes int64, filter RowFilter) (TableManifest, error) {
	rows, err := db.QueryContext(ctx, "select * from "+store.QuoteIdent(table))
	if err != nil {
		return TableManifest{}, fmt.Errorf("query table %s: %w", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return TableManifest{}, err
	}
	writer := &shardWriter{
		rootDir:       rootDir,
		relDir:        filepath.ToSlash(filepath.Join("tables", table)),
		maxShardBytes: maxShardBytes,
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "tables", table), 0o755); err != nil {
		return TableManifest{}, fmt.Errorf("create table dir %s: %w", table, err)
	}
	defer writer.close()
	enc := json.NewEncoder(writer)
	count := 0
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return TableManifest{}, fmt.Errorf("scan table %s: %w", table, err)
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = exportValue(values[i])
		}
		if filter != nil {
			keep, err := filter(table, row)
			if err != nil {
				return TableManifest{}, fmt.Errorf("filter table %s: %w", table, err)
			}
			if !keep {
				continue
			}
		}
		if err := writer.rotateIfNeeded(); err != nil {
			return TableManifest{}, err
		}
		if err := enc.Encode(row); err != nil {
			return TableManifest{}, fmt.Errorf("encode table %s: %w", table, err)
		}
		count++
		if err := writer.finishRow(); err != nil {
			return TableManifest{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return TableManifest{}, err
	}
	if err := writer.close(); err != nil {
		return TableManifest{}, err
	}
	return TableManifest{Name: table, Files: writer.files, Columns: cols, Rows: count}, nil
}

func importTable(ctx context.Context, tx *sql.Tx, rootDir string, table TableManifest) error {
	if len(table.Files) == 0 {
		return nil
	}
	for _, rel := range table.Files {
		path := filepath.Join(rootDir, filepath.FromSlash(rel))
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		if err := importJSONLGzip(ctx, tx, file, table.Name); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close %s: %w", rel, err)
		}
	}
	return nil
}

func importJSONLGzip(ctx context.Context, tx *sql.Tx, reader io.Reader, table string) error {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("open gzip for %s: %w", table, err)
	}
	defer gz.Close()
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return fmt.Errorf("decode %s row: %w", table, err)
		}
		if len(row) == 0 {
			continue
		}
		if err := insertRow(ctx, tx, table, row); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s rows: %w", table, err)
	}
	return nil
}

func insertRow(ctx context.Context, tx *sql.Tx, table string, row map[string]any) error {
	cols := make([]string, 0, len(row))
	for col := range row {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	quoted := make([]string, 0, len(cols))
	holders := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for _, col := range cols {
		quoted = append(quoted, store.QuoteIdent(col))
		holders = append(holders, "?")
		args = append(args, row[col])
	}
	stmt := fmt.Sprintf(
		"insert or replace into %s(%s) values(%s)",
		store.QuoteIdent(table),
		strings.Join(quoted, ","),
		strings.Join(holders, ","),
	)
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("insert %s row: %w", table, err)
	}
	return nil
}

type shardWriter struct {
	rootDir       string
	relDir        string
	maxShardBytes int64
	nextShard     int
	rowsInShard   int
	files         []string
	file          *os.File
	counter       *countingWriter
	gz            *gzip.Writer
}

func (w *shardWriter) Write(p []byte) (int, error) {
	if w.gz == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	return w.gz.Write(p)
}

func (w *shardWriter) open() error {
	rel := filepath.ToSlash(filepath.Join(w.relDir, fmt.Sprintf("%06d.jsonl.gz", w.nextShard)))
	path := filepath.Join(w.rootDir, filepath.FromSlash(rel))
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	w.nextShard++
	w.rowsInShard = 0
	w.files = append(w.files, rel)
	w.file = file
	w.counter = &countingWriter{w: file}
	w.gz = gzip.NewWriter(w.counter)
	return nil
}

func (w *shardWriter) rotateIfNeeded() error {
	if w.maxShardBytes <= 0 || w.rowsInShard == 0 || w.counter == nil || w.counter.n < w.maxShardBytes {
		return nil
	}
	if err := w.close(); err != nil {
		return err
	}
	return w.open()
}

func (w *shardWriter) finishRow() error {
	w.rowsInShard++
	if w.maxShardBytes > 1024*1024 && w.rowsInShard%1024 != 0 {
		return nil
	}
	if w.gz == nil {
		return nil
	}
	return w.gz.Flush()
}

func (w *shardWriter) close() error {
	var closeErr error
	if w.gz != nil {
		if err := w.gz.Close(); err != nil {
			closeErr = err
		}
		w.gz = nil
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		w.file = nil
	}
	if closeErr != nil {
		return fmt.Errorf("close shard: %w", closeErr)
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func exportValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}
