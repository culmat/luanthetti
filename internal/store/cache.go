package store

import (
	"database/sql"
	"errors"
	"time"
)

// CachedPackage is a ContentDB package detail cached by release id.
type CachedPackage struct {
	Key          string
	ReleaseID    int64
	FetchedAt    time.Time
	DetailJSON   []byte
	ReleasesJSON []byte
}

// CachedPackage returns the cached detail for a key, or ErrNotFound.
func (s *Store) CachedPackage(key string) (CachedPackage, error) {
	var c CachedPackage
	var fetched string
	err := s.db.QueryRow("SELECT key, release_id, fetched_at, detail_json, releases_json FROM cdb_packages WHERE key = ?", key).
		Scan(&c.Key, &c.ReleaseID, &fetched, &c.DetailJSON, &c.ReleasesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return CachedPackage{}, ErrNotFound
	}
	c.FetchedAt = parseTime(fetched)
	return c, err
}

// PutCachedPackage stores a package detail.
func (s *Store) PutCachedPackage(c CachedPackage) error {
	if c.DetailJSON == nil {
		c.DetailJSON = []byte{}
	}
	if c.ReleasesJSON == nil {
		c.ReleasesJSON = []byte{}
	}
	_, err := s.db.Exec(`INSERT INTO cdb_packages (key, release_id, fetched_at, detail_json, releases_json) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET release_id = excluded.release_id, fetched_at = excluded.fetched_at,
		detail_json = excluded.detail_json, releases_json = excluded.releases_json`,
		c.Key, c.ReleaseID, now(), c.DetailJSON, c.ReleasesJSON)
	return err
}

// CachedBlob returns a cached JSON blob and when it was fetched.
func (s *Store) CachedBlob(key string) ([]byte, time.Time, error) {
	var data []byte
	var fetched string
	err := s.db.QueryRow("SELECT json, fetched_at FROM cdb_blobs WHERE key = ?", key).Scan(&data, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, ErrNotFound
	}
	return data, parseTime(fetched), err
}

// PutCachedBlob stores a JSON blob.
func (s *Store) PutCachedBlob(key string, data []byte) error {
	_, err := s.db.Exec(`INSERT INTO cdb_blobs (key, fetched_at, json) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET fetched_at = excluded.fetched_at, json = excluded.json`, key, now(), data)
	return err
}

// DeleteCachedBlobs removes blobs whose key starts with prefix.
func (s *Store) DeleteCachedBlobs(prefix string) error {
	_, err := s.db.Exec("DELETE FROM cdb_blobs WHERE key LIKE ? || '%'", prefix)
	return err
}
