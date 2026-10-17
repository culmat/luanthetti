// Package store is Luanthetti's state: installations, library versions, links, pins, scan
// results and ContentDB caches, in one SQLite file.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store wraps the database.
type Store struct {
	db *sql.DB
}

// ErrNotFound is returned by single-row lookups.
var ErrNotFound = errors.New("not found")

// Open opens (creating if needed) the database at path and applies pending migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the connection for packages that keep their own tables (jobs, caches).
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)"); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE name = ?", name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		sqlText, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlText)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations VALUES (?, ?)", name, now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func parseList(s string) []string {
	var out []string
	if json.Unmarshal([]byte(s), &out) != nil || out == nil {
		return []string{}
	}
	return out
}

// Tx runs fn in a transaction.
func (s *Store) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Meta reads a meta value ("" when absent).
func (s *Store) Meta(key string) string {
	var v string
	_ = s.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	return v
}

// SetMeta writes a meta value.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec("INSERT INTO meta VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// Key normalises a ContentDB identity the way Luanti does: lower(author)/name.
func Key(author, name string) string { return strings.ToLower(author) + "/" + name }
