// Package store persists the journal in SQLite. Timestamps are stored as UTC
// plus the IANA zone they were recorded in, so the original local time can be
// shown later.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store is the SQLite-backed repository for every journal entity.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the database at path, applies pending
// migrations and returns a Store. now supplies the current time in the
// owner's time zone and is a parameter so tests can fix it.
func Open(path string, now func() time.Time) (*Store, error) {
	return openWith(path, now, migrationFiles)
}

func openWith(path string, now func() time.Time, migrations fs.FS) (*Store, error) {
	// Create the file ourselves so it is never world-readable, not even
	// briefly: the data is sensitive. SQLite copies this mode to -wal/-shm.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("restrict database file: %w", err)
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// One connection keeps SQLite writes serialized (single-user app) and
	// avoids "database is locked" errors. Never run a query while iterating
	// another one: collect rows first.
	db.SetMaxOpenConns(1)

	if err := migrate(db, path, migrations); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, now: now}, nil
}

func dsn(path string) string {
	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	return "file:" + escaped +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// withTx runs fn in a transaction, committing only if it returns nil.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() // no-op after a successful Commit
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func migrate(db *sql.DB, dbPath string, migrations fs.FS) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	// Glob returns names in lexical order, which is the order of the
	// zero-padded version prefixes.

	applied, err := appliedMigrations(db)
	if err != nil {
		return err
	}

	var pending []string
	for _, file := range files {
		if !applied[versionOf(file)] {
			pending = append(pending, file)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// Migrations are forward-only, so the backup is the way back. A brand-new
	// database has nothing worth backing up.
	if len(applied) > 0 {
		backup, err := backupDatabase(db, dbPath, versionOf(pending[0]))
		if err != nil {
			return fmt.Errorf("back up before migrating: %w", err)
		}
		slog.Info("database backed up before migration", "backup", backup, "pending", len(pending))
	}

	for _, file := range pending {
		if err := applyMigration(db, migrations, file); err != nil {
			return err
		}
		slog.Info("migration applied", "version", versionOf(file))
	}
	return nil
}

func versionOf(file string) string {
	name := file[strings.LastIndex(file, "/")+1:]
	return strings.TrimSuffix(name, ".sql")
}

func appliedMigrations(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func applyMigration(db *sql.DB, migrations fs.FS, file string) error {
	script, err := fs.ReadFile(migrations, file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", file, err)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", file, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(string(script)); err != nil {
		return fmt.Errorf("apply migration %s: %w", file, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		versionOf(file), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("record migration %s: %w", file, err)
	}
	return tx.Commit()
}

// backupDatabase writes a consistent snapshot next to the database file.
func backupDatabase(db *sql.DB, dbPath, nextVersion string) (string, error) {
	backup := fmt.Sprintf("%s.pre-%s.bak", dbPath, nextVersion)
	// VACUUM INTO needs a missing or empty target; creating it first lets us
	// set the restrictive mode before any data is written.
	f, err := os.OpenFile(backup, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	f.Close()
	if _, err := db.Exec(`VACUUM INTO ?`, backup); err != nil {
		return "", err
	}
	return backup, nil
}
