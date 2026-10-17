package store

import (
	"database/sql"
	"errors"
	"time"
)

// Package is a package known to the library.
type Package struct {
	ID               int64  `json:"id"`
	Author           string `json:"author"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Key              string `json:"key"`
	Title            string `json:"title"`
	ShortDescription string `json:"short_description"`
	Source           string `json:"source"`
	FolderType       string `json:"folder_type"`
	Folder           string `json:"folder"`
}

// Version is one release of a package in the library.
type Version struct {
	ID          int64     `json:"id"`
	PackageID   int64     `json:"package_id"`
	ReleaseID   int64     `json:"release_id"`
	Path        string    `json:"path"`
	Title       string    `json:"title"`
	MinEngine   string    `json:"min_engine,omitempty"`
	MaxEngine   string    `json:"max_engine,omitempty"`
	ReleaseDate string    `json:"release_date,omitempty"`
	Origin      string    `json:"origin"`
	SHA256      string    `json:"sha256,omitempty"`
	AddedAt     time.Time `json:"added_at"`
	Complete    bool      `json:"complete"`
}

const packageCols = "id, author, name, type, key, title, short_description, source, folder_type, folder"

func scanPackage(sc interface{ Scan(...any) error }) (Package, error) {
	var p Package
	err := sc.Scan(&p.ID, &p.Author, &p.Name, &p.Type, &p.Key, &p.Title, &p.ShortDescription, &p.Source, &p.FolderType, &p.Folder)
	return p, err
}

// UpsertPackage inserts or updates a package by key and returns it with its id.
func (s *Store) UpsertPackage(p Package) (Package, error) {
	_, err := s.db.Exec(`INSERT INTO packages (author, name, type, title, short_description, source, folder_type, folder)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET author = excluded.author, type = excluded.type, title = excluded.title,
		short_description = excluded.short_description, source = excluded.source, folder_type = excluded.folder_type, folder = excluded.folder`,
		p.Author, p.Name, p.Type, p.Title, p.ShortDescription, p.Source, p.FolderType, p.Folder)
	if err != nil {
		return Package{}, err
	}
	return s.PackageByKey(Key(p.Author, p.Name))
}

// PackageByKey finds a package by lower(author)/name.
func (s *Store) PackageByKey(key string) (Package, error) {
	p, err := scanPackage(s.db.QueryRow("SELECT "+packageCols+" FROM packages WHERE key = ?", key))
	if errors.Is(err, sql.ErrNoRows) {
		return Package{}, ErrNotFound
	}
	return p, err
}

// PackageByID finds a package by id.
func (s *Store) PackageByID(id int64) (Package, error) {
	p, err := scanPackage(s.db.QueryRow("SELECT "+packageCols+" FROM packages WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Package{}, ErrNotFound
	}
	return p, err
}

// Packages lists every package, by key.
func (s *Store) Packages() ([]Package, error) {
	rows, err := s.db.Query("SELECT " + packageCols + " FROM packages ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Package
	for rows.Next() {
		p, err := scanPackage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePackage removes a package and its versions (links must be gone first).
func (s *Store) DeletePackage(id int64) error {
	_, err := s.db.Exec("DELETE FROM packages WHERE id = ?", id)
	return err
}

const versionCols = "id, package_id, release_id, path, title, min_engine, max_engine, release_date, origin, sha256, added_at, complete"

func scanVersion(sc interface{ Scan(...any) error }) (Version, error) {
	var v Version
	var added string
	var complete int
	err := sc.Scan(&v.ID, &v.PackageID, &v.ReleaseID, &v.Path, &v.Title, &v.MinEngine, &v.MaxEngine, &v.ReleaseDate, &v.Origin, &v.SHA256, &added, &complete)
	v.AddedAt, v.Complete = parseTime(added), complete != 0
	return v, err
}

// AddVersion records a version (replacing an existing row for the same release).
func (s *Store) AddVersion(v Version) (Version, error) {
	complete := 0
	if v.Complete {
		complete = 1
	}
	_, err := s.db.Exec(`INSERT INTO versions (package_id, release_id, path, title, min_engine, max_engine, release_date, origin, sha256, added_at, complete)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (package_id, release_id) DO UPDATE SET path = excluded.path, title = excluded.title, min_engine = excluded.min_engine,
		max_engine = excluded.max_engine, release_date = excluded.release_date, origin = excluded.origin, sha256 = excluded.sha256,
		added_at = excluded.added_at, complete = excluded.complete`,
		v.PackageID, v.ReleaseID, v.Path, v.Title, v.MinEngine, v.MaxEngine, v.ReleaseDate, v.Origin, v.SHA256, now(), complete)
	if err != nil {
		return Version{}, err
	}
	return s.VersionByRelease(v.PackageID, v.ReleaseID)
}

// MarkComplete flags a version as fully downloaded.
func (s *Store) MarkComplete(id int64, complete bool) error {
	c := 0
	if complete {
		c = 1
	}
	_, err := s.db.Exec("UPDATE versions SET complete = ? WHERE id = ?", c, id)
	return err
}

// VersionByRelease finds a package's version by release id.
func (s *Store) VersionByRelease(packageID, releaseID int64) (Version, error) {
	v, err := scanVersion(s.db.QueryRow("SELECT "+versionCols+" FROM versions WHERE package_id = ? AND release_id = ?", packageID, releaseID))
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	return v, err
}

// VersionByID finds a version by id.
func (s *Store) VersionByID(id int64) (Version, error) {
	v, err := scanVersion(s.db.QueryRow("SELECT "+versionCols+" FROM versions WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	return v, err
}

// VersionsOf lists a package's versions, newest release first.
func (s *Store) VersionsOf(packageID int64) ([]Version, error) {
	rows, err := s.db.Query("SELECT "+versionCols+" FROM versions WHERE package_id = ? ORDER BY release_id DESC", packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteVersion removes a version row (the caller removes the files).
func (s *Store) DeleteVersion(id int64) error {
	_, err := s.db.Exec("DELETE FROM versions WHERE id = ?", id)
	return err
}

// LinkCount is how many links point at a version.
func (s *Store) LinkCount(versionID int64) (int, error) {
	var n int
	err := s.db.QueryRow("SELECT count(*) FROM links WHERE version_id = ?", versionID).Scan(&n)
	return n, err
}
