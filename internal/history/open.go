// Package history persists immutable completed reports and durable run intents.
package history

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const OperationTimeout = 3 * time.Second
const applicationID = 0x464c4e45

//go:embed migrations/001_initial.sql
var schemaV1 string
var migrations = []string{schemaV1}

type Store struct{ db *sql.DB }

// Open creates/migrates an explicitly selected local file. OpenExisting never
// creates a file or migrates it, but permits SQLite hot-journal recovery writes.
func Open(ctx context.Context, path string) (*Store, error)         { return open(ctx, path, true) }
func OpenExisting(ctx context.Context, path string) (*Store, error) { return open(ctx, path, false) }
func (s *Store) Close() error                                       { return s.db.Close() }

func open(ctx context.Context, path string, create bool) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, fmt.Errorf("history requires a local database file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if create {
		if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err == nil {
			_ = f.Close()
		} else if !os.IsExist(err) {
			return nil, err
		}
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("history database must be a regular file, not a symlink")
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := url.Values{"mode": {"rw"}, "_busy_timeout": {"500"}, "_foreign_keys": {"on"}, "_synchronous": {"EXTRA"}, "_txlock": {"immediate"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	failed := true
	defer func() {
		if failed {
			_ = db.Close()
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open history: %w", err)
	}
	if create {
		err = migrate(ctx, db, migrations)
	} else {
		err = checkVersion(ctx, db)
	}
	if err != nil {
		return nil, err
	}
	var journal string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		return nil, err
	}
	if journal != "delete" {
		return nil, fmt.Errorf("history requires DELETE journal mode, found %s", journal)
	}
	failed = false
	return &Store{db: db}, nil
}

func checkVersion(ctx context.Context, db *sql.DB) error {
	var app, version int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if app != applicationID || version != len(migrations) {
		return fmt.Errorf("unsupported or unrelated history database (application=%d, version=%d)", app, version)
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB, steps []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var app, version, objects int
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if app == 0 && version == 0 {
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
			return err
		}
		if objects != 0 {
			return fmt.Errorf("refusing to initialize an unrelated SQLite database")
		}
	} else if app != applicationID {
		return fmt.Errorf("unrelated SQLite application ID %d", app)
	}
	if version < 0 || version > len(steps) {
		return fmt.Errorf("unsupported history schema version %d", version)
	}
	for i := version; i < len(steps); i++ {
		if _, err := tx.ExecContext(ctx, steps[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d", applicationID)); err != nil {
		return err
	}
	return tx.Commit()
}
