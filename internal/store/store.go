package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/maximusjb/crate/internal/store/db"
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

func (s *Store) Q() *db.Queries { return s.q }
func (s *Store) Close() error   { return s.sql.Close() }
