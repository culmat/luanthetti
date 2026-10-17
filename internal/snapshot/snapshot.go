// Package snapshot keeps a local copy of luanti-depgraph's nightly database: it probes the
// tiny info.json beside the release asset at most every ProbeInterval, downloads the
// zstd-compressed database when a newer build exists, verifies it (PRAGMA quick_check and the
// schema contract) and swaps it in atomically. Everything keeps working offline.
package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/culmat/luanthetti/internal/depgraph"
)

// FileName is the database file inside Dir.
const FileName = "luanti-deps.sqlite"

// MaxSize caps the decompressed download.
const MaxSize = 512 << 20

// Manager downloads and opens the snapshot.
type Manager struct {
	Dir           string
	URL           string       // the .sqlite.zst asset; LUANTHETTI_SNAPSHOT_URL overrides it
	InfoURL       string       // info.json beside it; derived from URL when empty
	Client        *http.Client // must set the User-Agent via Transport or UserAgent below
	UserAgent     string
	Offline       bool
	ProbeInterval time.Duration // default 20h
	StaleAfter    time.Duration // default 72h; Stale() warns beyond it
	Log           func(format string, args ...any)
}

// State is the sidecar state.json next to the database.
type State struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	DownloadedAt time.Time `json:"downloaded_at"`
	LastProbeAt  time.Time `json:"last_probe_at"`
	BuiltAt      time.Time `json:"built_at"`
}

// Result describes what Refresh did.
type Result struct {
	Path       string    `json:"path"`
	Downloaded bool      `json:"downloaded"`
	BuiltAt    time.Time `json:"built_at"`
	Probed     bool      `json:"probed"`
	Offline    bool      `json:"offline"`
	Note       string    `json:"note,omitempty"`
}

// ErrNoSnapshot means there is no local database and nothing could be downloaded.
var ErrNoSnapshot = errors.New("no dependency snapshot; run `luanthetti refresh` when online")

// Path is the database file.
func (m *Manager) Path() string { return filepath.Join(m.Dir, FileName) }

func (m *Manager) statePath() string { return filepath.Join(m.Dir, "state.json") }

func (m *Manager) infoURL() string {
	if m.InfoURL != "" {
		return m.InfoURL
	}
	if i := strings.LastIndex(m.URL, "/"); i >= 0 {
		return m.URL[:i+1] + "info.json"
	}
	return ""
}

func (m *Manager) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(format, args...)
	}
}

func (m *Manager) probeInterval() time.Duration {
	if m.ProbeInterval <= 0 {
		return 20 * time.Hour
	}
	return m.ProbeInterval
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Timeout: 120 * time.Second}
}

func (m *Manager) readState() State {
	var st State
	if data, err := os.ReadFile(m.statePath()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func (m *Manager) writeState(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.statePath(), data, 0o644)
}

// Open opens the local database and validates it. warning is non-empty for a newer
// rules_version than this build knows.
func Open(path string) (idx *depgraph.Index, meta depgraph.Meta, warning string, err error) {
	idx, err = depgraph.Open(path)
	if err != nil {
		return nil, depgraph.Meta{}, "", err
	}
	meta, err = depgraph.ReadMeta(idx.DB())
	if err != nil {
		idx.Close()
		return nil, depgraph.Meta{}, "", err
	}
	warning, err = depgraph.CheckSchema(idx.DB(), meta)
	if err != nil {
		idx.Close()
		return nil, depgraph.Meta{}, "", err
	}
	return idx, meta, warning, nil
}

// Ensure makes sure a usable database exists (downloading or refreshing as the probe interval
// allows) and opens it.
func (m *Manager) Ensure(ctx context.Context) (*depgraph.Index, depgraph.Meta, Result, error) {
	res, err := m.Refresh(ctx, false)
	if err != nil {
		return nil, depgraph.Meta{}, res, err
	}
	idx, meta, warning, err := Open(m.Path())
	if err != nil {
		return nil, depgraph.Meta{}, res, err
	}
	if warning != "" {
		m.logf("%s", warning)
	}
	return idx, meta, res, nil
}

// Refresh downloads a newer snapshot when one exists. With force it skips the probe interval.
// A missing network is not an error when a local database exists.
func (m *Manager) Refresh(ctx context.Context, force bool) (Result, error) {
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return Result{}, err
	}
	st := m.readState()
	res := Result{Path: m.Path(), BuiltAt: st.BuiltAt, Offline: m.Offline}
	_, statErr := os.Stat(m.Path())
	have := statErr == nil
	if !have {
		if m.Offline {
			return res, ErrNoSnapshot
		}
		changed, err := m.download(ctx, &st, time.Time{})
		if err != nil {
			return res, fmt.Errorf("downloading the dependency snapshot: %w", err)
		}
		res.Downloaded, res.BuiltAt = changed, st.BuiltAt
		return res, nil
	}
	if m.Offline {
		return res, nil
	}
	if !force && time.Since(st.LastProbeAt) < m.probeInterval() {
		return res, nil
	}
	remote, err := m.Probe(ctx)
	res.Probed = true
	if err != nil {
		// Offline or GitHub unreachable: keep the current file, don't retry for an hour.
		m.logf("dependency snapshot: cannot check for updates (%v); using the copy built %s", err, st.BuiltAt.Format("2006-01-02"))
		st.LastProbeAt = time.Now().Add(-m.probeInterval()).Add(time.Hour)
		_ = m.writeState(st)
		res.Note = "update check failed: " + err.Error()
		return res, nil
	}
	if !remote.After(st.BuiltAt) {
		st.LastProbeAt = time.Now()
		_ = m.writeState(st)
		return res, nil
	}
	changed, err := m.download(ctx, &st, st.BuiltAt)
	if err != nil {
		m.logf("dependency snapshot: download failed (%v); using the copy built %s", err, st.BuiltAt.Format("2006-01-02"))
		res.Note = "download failed: " + err.Error()
		return res, nil
	}
	res.Downloaded, res.BuiltAt = changed, st.BuiltAt
	return res, nil
}

// Probe fetches info.json and returns the remote built_at.
func (m *Manager) Probe(ctx context.Context) (time.Time, error) {
	url := m.infoURL()
	if url == "" {
		return time.Time{}, errors.New("no info URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	m.setUA(req)
	resp, err := m.client().Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var info struct {
		BuiltAt string `json:"built_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return time.Time{}, fmt.Errorf("info.json: %w", err)
	}
	t, err := time.Parse(time.RFC3339, strings.Trim(info.BuiltAt, `"`))
	if err != nil {
		return time.Time{}, fmt.Errorf("info.json built_at: %w", err)
	}
	return t, nil
}

func (m *Manager) setUA(req *http.Request) {
	if m.UserAgent != "" {
		req.Header.Set("User-Agent", m.UserAgent)
	}
}

// download fetches, verifies and installs the database. It returns false when the server
// answered 304 or the file is not newer than currentBuiltAt.
func (m *Manager) download(ctx context.Context, st *State, currentBuiltAt time.Time) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return false, err
	}
	m.setUA(req)
	if st.ETag != "" && st.URL == m.URL {
		req.Header.Set("If-None-Match", st.ETag)
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		st.LastProbeAt = time.Now()
		return false, m.writeState(*st)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET %s: %s", m.URL, resp.Status)
	}
	tmp := m.Path() + ".download"
	defer os.Remove(tmp)
	if err := decompressTo(tmp, resp.Body); err != nil {
		return false, err
	}
	meta, err := verify(tmp)
	if err != nil {
		return false, err
	}
	if !currentBuiltAt.IsZero() && !meta.BuiltAt.After(currentBuiltAt) {
		st.LastProbeAt = time.Now()
		return false, m.writeState(*st)
	}
	if err := os.Rename(tmp, m.Path()); err != nil {
		return false, err
	}
	*st = State{URL: m.URL, ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"),
		DownloadedAt: time.Now(), LastProbeAt: time.Now(), BuiltAt: meta.BuiltAt}
	m.logf("dependency snapshot: downloaded the build from %s (%d packages)", meta.BuiltAt.Format("2006-01-02"), meta.Packages)
	return true, m.writeState(*st)
}

func decompressTo(path string, body io.Reader) error {
	dec, err := zstd.NewReader(body)
	if err != nil {
		return err
	}
	defer dec.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(dec, MaxSize+1))
	if err == nil && n > MaxSize {
		err = errors.New("snapshot larger than the size limit")
	}
	if err2 := f.Sync(); err == nil {
		err = err2
	}
	if err2 := f.Close(); err == nil {
		err = err2
	}
	return err
}

// verify runs PRAGMA quick_check, reads meta and checks the schema contract.
func verify(path string) (depgraph.Meta, error) {
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return depgraph.Meta{}, err
	}
	defer conn.Close()
	var check string
	if err := conn.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		return depgraph.Meta{}, fmt.Errorf("downloaded snapshot failed the SQLite integrity check (%s %v)", check, err)
	}
	meta, err := depgraph.ReadMeta(conn)
	if err != nil {
		return depgraph.Meta{}, err
	}
	if _, err := depgraph.CheckSchema(conn, meta); err != nil {
		return depgraph.Meta{}, err
	}
	if meta.BuiltAt.IsZero() {
		return depgraph.Meta{}, errors.New("downloaded snapshot has no built_at")
	}
	return meta, nil
}

// Stale returns the snapshot's age and whether it is older than StaleAfter (72h by default).
func (m *Manager) Stale(meta depgraph.Meta, now time.Time) (time.Duration, bool) {
	after := m.StaleAfter
	if after <= 0 {
		after = 72 * time.Hour
	}
	age := now.Sub(meta.BuiltAt)
	return age, age > after
}
