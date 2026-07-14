package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrateMu sync.Mutex

type Store struct {
	sql  *sql.DB
	q    *db.Queries
	path string
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
	return &Store{sql: conn, q: db.New(conn), path: path}, nil
}

func (s *Store) Migrate() error {
	migrateMu.Lock()
	defer migrateMu.Unlock()
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("sqlite"); err != nil {
		return err
	}
	s.backupBeforePendingMigrations()
	return goose.Up(s.sql, "migrations")
}

// backupBeforePendingMigrations snapshots the database to a sibling
// `<db>.pre-migrate-v<N>.bak` file when a schema upgrade is about to run against
// an existing, non-empty database — so a failed or unwanted migration is
// recoverable by copying the snapshot back. It is best-effort: a backup failure
// logs a warning and does NOT abort startup, since a transient copy error must
// not brick the container in a crash loop.
func (s *Store) backupBeforePendingMigrations() {
	if s.path == "" || s.path == ":memory:" {
		return
	}
	cur, err := goose.GetDBVersion(s.sql)
	if err != nil || cur <= 0 {
		return // fresh DB (no goose table yet) or empty — nothing worth preserving
	}
	if latestMigrationVersion() <= cur {
		return // already current — goose.Up will be a no-op
	}
	backup := fmt.Sprintf("%s.pre-migrate-v%d.bak", s.path, cur)
	_ = os.Remove(backup) // VACUUM INTO requires the target file not to exist
	quoted := strings.ReplaceAll(backup, "'", "''")
	if _, err := s.sql.Exec("VACUUM INTO '" + quoted + "'"); err != nil {
		log.Printf("WARNING: pre-migration DB backup to %s failed: %v (continuing)", backup, err)
		return
	}
	log.Printf("pre-migration DB backup written: %s (schema v%d, upgrading)", backup, cur)
}

// latestMigrationVersion returns the highest numeric prefix among the embedded
// migration files (e.g. 22 for `0022_download_job_canonical.sql`), or 0 if none.
func latestMigrationVersion() int64 {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return 0
	}
	var max int64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		idx := strings.IndexByte(name, '_')
		if idx <= 0 {
			continue
		}
		v, perr := strconv.ParseInt(name[:idx], 10, 64)
		if perr == nil && v > max {
			max = v
		}
	}
	return max
}

func (s *Store) Q() *db.Queries { return s.q }
func (s *Store) Close() error   { return s.sql.Close() }
func (s *Store) DB() *sql.DB    { return s.sql }

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
