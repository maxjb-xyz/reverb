package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrateMu sync.Mutex

type Store struct {
	sql *sql.DB
	q   *db.Queries
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	conn, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	return &Store{sql: conn, q: db.New(conn)}, nil
}

func (s *Store) Migrate() error {
	migrateMu.Lock()
	defer migrateMu.Unlock()
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("sqlite"); err != nil {
		return err
	}
	return goose.Up(s.sql, "migrations")
}

func (s *Store) Q() *db.Queries   { return s.q }
func (s *Store) Close() error      { return s.sql.Close() }
func (s *Store) DB() *sql.DB       { return s.sql }

// LibraryVersion returns the monotonic library_version from settings, returning
// 1 when the key is absent or unparseable. A match_cache row is stale iff its
// library_version is below this value.
func (s *Store) LibraryVersion(ctx context.Context) (int64, error) {
	v, err := s.q.GetLibraryVersion(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return 1, nil
		}
		return 0, err
	}
	n, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		return 1, nil
	}
	return n, nil
}

// SetLibraryVersion writes the monotonic library_version into settings. The
// Manager bumps it on scan-completion to invalidate stale match_cache rows.
func (s *Store) SetLibraryVersion(ctx context.Context, v int64) error {
	return s.q.SetLibraryVersion(ctx, strconv.FormatInt(v, 10))
}
