package store

import (
	"database/sql"
	"errors"
	"time"
)

// Installation is a registered Luanti installation.
type Installation struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Root          string    `json:"root"`
	Kind          string    `json:"kind"`
	EngineVersion string    `json:"engine_version,omitempty"`
	Protocol      int       `json:"protocol,omitempty"`
	EngineSource  string    `json:"engine_source,omitempty"`
	BinaryPath    string    `json:"binary_path,omitempty"`
	ExtraGameDirs []string  `json:"extra_game_dirs"`
	LinkMode      string    `json:"link_mode"`
	AddedAt       time.Time `json:"added_at"`
	LastScanAt    time.Time `json:"last_scan_at,omitempty"`
}

// Link is a package version placed into an installation.
type Link struct {
	ID             int64     `json:"id"`
	InstallationID int64     `json:"installation_id"`
	PackageID      int64     `json:"package_id"`
	VersionID      int64     `json:"version_id"`
	Path           string    `json:"path"`
	Kind           string    `json:"kind"`
	Reason         string    `json:"reason"`
	RequiredBy     []string  `json:"required_by"`
	PendingReplace string    `json:"pending_replace,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Pin holds a package in an installation at its current or a given release.
type Pin struct {
	InstallationID int64  `json:"installation_id"`
	PackageID      int64  `json:"package_id"`
	ReleaseID      *int64 `json:"release_id"`
}

// ScanResult is one entry found by scanning an installation.
type ScanResult struct {
	ID             int64     `json:"id"`
	InstallationID int64     `json:"installation_id"`
	ScannedAt      time.Time `json:"scanned_at"`
	Path           string    `json:"path"`
	FolderType     string    `json:"folder_type,omitempty"`
	Status         string    `json:"status"`
	Author         string    `json:"author,omitempty"`
	Name           string    `json:"name,omitempty"`
	ReleaseID      int64     `json:"release_id,omitempty"`
	Detail         string    `json:"detail,omitempty"`
}

const instCols = "id, name, root, kind, engine_version, protocol, engine_source, binary_path, extra_game_dirs, link_mode, added_at, last_scan_at"

func scanInst(sc interface{ Scan(...any) error }) (Installation, error) {
	var i Installation
	var extra, added, scanned string
	err := sc.Scan(&i.ID, &i.Name, &i.Root, &i.Kind, &i.EngineVersion, &i.Protocol, &i.EngineSource, &i.BinaryPath, &extra, &i.LinkMode, &added, &scanned)
	i.ExtraGameDirs, i.AddedAt, i.LastScanAt = parseList(extra), parseTime(added), parseTime(scanned)
	return i, err
}

// AddInstallation registers an installation.
func (s *Store) AddInstallation(i Installation) (Installation, error) {
	if i.LinkMode == "" {
		i.LinkMode = "auto"
	}
	res, err := s.db.Exec(`INSERT INTO installations (name, root, kind, engine_version, protocol, engine_source, binary_path, extra_game_dirs, link_mode, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		i.Name, i.Root, i.Kind, i.EngineVersion, i.Protocol, i.EngineSource, i.BinaryPath, jsonList(i.ExtraGameDirs), i.LinkMode, now())
	if err != nil {
		return Installation{}, err
	}
	id, _ := res.LastInsertId()
	return s.InstallationByID(id)
}

// UpdateInstallation saves changed fields of an installation.
func (s *Store) UpdateInstallation(i Installation) error {
	_, err := s.db.Exec(`UPDATE installations SET name = ?, root = ?, kind = ?, engine_version = ?, protocol = ?, engine_source = ?,
		binary_path = ?, extra_game_dirs = ?, link_mode = ?, last_scan_at = ? WHERE id = ?`,
		i.Name, i.Root, i.Kind, i.EngineVersion, i.Protocol, i.EngineSource, i.BinaryPath, jsonList(i.ExtraGameDirs), i.LinkMode,
		timeString(i.LastScanAt), i.ID)
	return err
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// InstallationByID finds an installation.
func (s *Store) InstallationByID(id int64) (Installation, error) {
	i, err := scanInst(s.db.QueryRow("SELECT "+instCols+" FROM installations WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Installation{}, ErrNotFound
	}
	return i, err
}

// Installation finds an installation by name or root path.
func (s *Store) Installation(nameOrRoot string) (Installation, error) {
	i, err := scanInst(s.db.QueryRow("SELECT "+instCols+" FROM installations WHERE name = ? OR root = ?", nameOrRoot, nameOrRoot))
	if errors.Is(err, sql.ErrNoRows) {
		return Installation{}, ErrNotFound
	}
	return i, err
}

// Installations lists every installation by name.
func (s *Store) Installations() ([]Installation, error) {
	rows, err := s.db.Query("SELECT " + instCols + " FROM installations ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Installation{}
	for rows.Next() {
		i, err := scanInst(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// DeleteInstallation removes an installation and, through cascades, its links, pins and scans.
func (s *Store) DeleteInstallation(id int64) error {
	_, err := s.db.Exec("DELETE FROM installations WHERE id = ?", id)
	return err
}

const linkCols = "id, installation_id, package_id, version_id, path, kind, reason, required_by, pending_replace, created_at"

func scanLink(sc interface{ Scan(...any) error }) (Link, error) {
	var l Link
	var req, created string
	err := sc.Scan(&l.ID, &l.InstallationID, &l.PackageID, &l.VersionID, &l.Path, &l.Kind, &l.Reason, &req, &l.PendingReplace, &created)
	l.RequiredBy, l.CreatedAt = parseList(req), parseTime(created)
	return l, err
}

// AddLink records a link (replacing a row at the same path).
func (s *Store) AddLink(l Link) (Link, error) {
	_, err := s.db.Exec(`INSERT INTO links (installation_id, package_id, version_id, path, kind, reason, required_by, pending_replace, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (installation_id, path) DO UPDATE SET package_id = excluded.package_id, version_id = excluded.version_id,
		kind = excluded.kind, reason = excluded.reason, required_by = excluded.required_by, pending_replace = excluded.pending_replace`,
		l.InstallationID, l.PackageID, l.VersionID, l.Path, l.Kind, l.Reason, jsonList(l.RequiredBy), l.PendingReplace, now())
	if err != nil {
		return Link{}, err
	}
	return s.LinkAt(l.InstallationID, l.Path)
}

// LinkAt finds the link at a path in an installation.
func (s *Store) LinkAt(installationID int64, path string) (Link, error) {
	l, err := scanLink(s.db.QueryRow("SELECT "+linkCols+" FROM links WHERE installation_id = ? AND path = ?", installationID, path))
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	return l, err
}

func (s *Store) links(where string, args ...any) ([]Link, error) {
	rows, err := s.db.Query("SELECT "+linkCols+" FROM links WHERE "+where+" ORDER BY path", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LinksIn lists the links of an installation.
func (s *Store) LinksIn(installationID int64) ([]Link, error) {
	return s.links("installation_id = ?", installationID)
}

// LinksTo lists the links pointing at a version.
func (s *Store) LinksTo(versionID int64) ([]Link, error) { return s.links("version_id = ?", versionID) }

// LinksOf lists every link of a package across installations.
func (s *Store) LinksOf(packageID int64) ([]Link, error) { return s.links("package_id = ?", packageID) }

// AllLinks lists every link.
func (s *Store) AllLinks() ([]Link, error) { return s.links("1 = 1") }

// UpdateLink saves a link's version, kind, reason, required_by and pending_replace.
func (s *Store) UpdateLink(l Link) error {
	_, err := s.db.Exec("UPDATE links SET version_id = ?, kind = ?, reason = ?, required_by = ?, pending_replace = ? WHERE id = ?",
		l.VersionID, l.Kind, l.Reason, jsonList(l.RequiredBy), l.PendingReplace, l.ID)
	return err
}

// RemoveLink deletes a link row.
func (s *Store) RemoveLink(id int64) error {
	_, err := s.db.Exec("DELETE FROM links WHERE id = ?", id)
	return err
}

// SetPin pins a package in an installation (release nil = hold the current one).
func (s *Store) SetPin(p Pin) error {
	_, err := s.db.Exec(`INSERT INTO pins VALUES (?, ?, ?) ON CONFLICT (installation_id, package_id) DO UPDATE SET release_id = excluded.release_id`,
		p.InstallationID, p.PackageID, p.ReleaseID)
	return err
}

// Unpin removes a pin.
func (s *Store) Unpin(installationID, packageID int64) error {
	_, err := s.db.Exec("DELETE FROM pins WHERE installation_id = ? AND package_id = ?", installationID, packageID)
	return err
}

// Pin returns the pin for a package in an installation, if any.
func (s *Store) Pin(installationID, packageID int64) (*Pin, error) {
	var p Pin
	var rel sql.NullInt64
	err := s.db.QueryRow("SELECT installation_id, package_id, release_id FROM pins WHERE installation_id = ? AND package_id = ?", installationID, packageID).
		Scan(&p.InstallationID, &p.PackageID, &rel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rel.Valid {
		v := rel.Int64
		p.ReleaseID = &v
	}
	return &p, nil
}

// Pins lists every pin.
func (s *Store) Pins() ([]Pin, error) {
	rows, err := s.db.Query("SELECT installation_id, package_id, release_id FROM pins")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pin{}
	for rows.Next() {
		var p Pin
		var rel sql.NullInt64
		if err := rows.Scan(&p.InstallationID, &p.PackageID, &rel); err != nil {
			return nil, err
		}
		if rel.Valid {
			v := rel.Int64
			p.ReleaseID = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReplaceScanResults stores the results of a scan, replacing the previous ones.
func (s *Store) ReplaceScanResults(installationID int64, results []ScanResult) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM scan_results WHERE installation_id = ?", installationID); err != nil {
		tx.Rollback()
		return err
	}
	ts := now()
	for _, r := range results {
		if _, err := tx.Exec(`INSERT INTO scan_results (installation_id, scanned_at, path, folder_type, status, author, name, release_id, detail)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, installationID, ts, r.Path, r.FolderType, r.Status, r.Author, r.Name, r.ReleaseID, r.Detail); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec("UPDATE installations SET last_scan_at = ? WHERE id = ?", ts, installationID); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ScanResults lists the stored results of the last scan.
func (s *Store) ScanResults(installationID int64) ([]ScanResult, error) {
	rows, err := s.db.Query(`SELECT id, installation_id, scanned_at, path, folder_type, status, author, name, release_id, detail
		FROM scan_results WHERE installation_id = ? ORDER BY path`, installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScanResult{}
	for rows.Next() {
		var r ScanResult
		var scanned string
		if err := rows.Scan(&r.ID, &r.InstallationID, &scanned, &r.Path, &r.FolderType, &r.Status, &r.Author, &r.Name, &r.ReleaseID, &r.Detail); err != nil {
			return nil, err
		}
		r.ScannedAt = parseTime(scanned)
		out = append(out, r)
	}
	return out, rows.Err()
}
