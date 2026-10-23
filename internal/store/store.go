// Package store owns the SQLite database: schema, models and all queries.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

const schemaV1 = `
CREATE TABLE IF NOT EXISTS groups (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL,
  sort       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS nodes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id   INTEGER REFERENCES groups(id) ON DELETE SET NULL,
  name       TEXT    NOT NULL,
  type       TEXT    NOT NULL CHECK (type IN ('icmp','tcp')),
  host       TEXT    NOT NULL,
  port       INTEGER NOT NULL DEFAULT 0,
  interval_s INTEGER NOT NULL DEFAULT 30,
  count      INTEGER NOT NULL DEFAULT 5,
  timeout_ms INTEGER NOT NULL DEFAULT 2000,
  family     TEXT    NOT NULL DEFAULT 'auto',
  enabled    INTEGER NOT NULL DEFAULT 1,
  sort       INTEGER NOT NULL DEFAULT 0,
  note       TEXT    NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_nodes_group ON nodes(group_id, sort);

CREATE TABLE IF NOT EXISTS samples_raw (
  node_id    INTEGER NOT NULL,
  ts         INTEGER NOT NULL,
  sent       INTEGER NOT NULL,
  recv       INTEGER NOT NULL,
  rtt_min_us INTEGER NOT NULL DEFAULT 0,
  rtt_avg_us INTEGER NOT NULL DEFAULT 0,
  rtt_max_us INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, ts)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS rollup_5m (
  node_id    INTEGER NOT NULL,
  ts         INTEGER NOT NULL,
  sent       INTEGER NOT NULL,
  recv       INTEGER NOT NULL,
  rtt_sum_us INTEGER NOT NULL DEFAULT 0,
  rtt_min_us INTEGER NOT NULL DEFAULT 0,
  rtt_max_us INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, ts)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS rollup_1h (
  node_id    INTEGER NOT NULL,
  ts         INTEGER NOT NULL,
  sent       INTEGER NOT NULL,
  recv       INTEGER NOT NULL,
  rtt_sum_us INTEGER NOT NULL DEFAULT 0,
  rtt_min_us INTEGER NOT NULL DEFAULT 0,
  rtt_max_us INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, ts)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

// Open opens (creating if needed) the database file and runs migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc's driver serialises writers; one connection keeps reads and
	// writes deterministic and removes SQLITE_BUSY surprises.
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(10 * time.Minute)

	s := &Store{db: db, path: path}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Path() string { return s.path }

// DiskSize reports the size of the database plus its WAL file.
func (s *Store) DiskSize() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(s.path + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version >= 1 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range strings.Split(schemaV1, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 1`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Tx runs fn inside a transaction.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

var ErrNotFound = errors.New("not found")
