package snapshot

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

const mini = "../depgraph/testdata/mini.sqlite"

// server serves a zstd snapshot and its info.json, counting requests.
type server struct {
	db       []byte
	builtAt  string
	etag     string
	requests atomic.Int32
	*httptest.Server
}

func newServer(t *testing.T, db []byte, builtAt string) *server {
	t.Helper()
	s := &server{db: db, builtAt: builtAt, etag: `"` + builtAt + `"`}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		if r.Header.Get("User-Agent") != "luanthetti-test/1" {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/info.json":
			w.Write([]byte(`{"built_at": "` + s.builtAt + `", "packages": 10}`))
		case "/luanti-deps.sqlite.zst":
			if r.Header.Get("If-None-Match") == s.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", s.etag)
			enc, _ := zstd.NewWriter(w)
			enc.Write(s.db)
			enc.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func manager(t *testing.T, s *server, dir string) *Manager {
	return &Manager{Dir: dir, URL: s.URL + "/luanti-deps.sqlite.zst", UserAgent: "luanthetti-test/1",
		Client: s.Client(), ProbeInterval: 20 * time.Hour, Log: t.Logf}
}

// withBuiltAt returns a copy of the fixture database with another meta.built_at.
func withBuiltAt(t *testing.T, builtAt string) []byte {
	t.Helper()
	src, err := os.ReadFile(mini)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "copy.sqlite")
	os.WriteFile(path, src, 0o644)
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE meta SET value = ? WHERE key = 'built_at'`, `"`+builtAt+`"`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	out, _ := os.ReadFile(path)
	return out
}

func TestEnsureDownloadsProbesAndUpdates(t *testing.T) {
	ctx := context.Background()
	old := withBuiltAt(t, "2026-10-10T03:17:00+00:00")
	s := newServer(t, old, "2026-10-10T03:17:00+00:00")
	dir := t.TempDir()
	m := manager(t, s, dir)

	idx, meta, res, err := m.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	idx.Close()
	if !res.Downloaded || meta.Packages != 10 || meta.BuiltAt.Format("2006-01-02") != "2026-10-10" {
		t.Fatalf("%+v %+v", res, meta)
	}
	if n := s.requests.Load(); n != 1 {
		t.Errorf("first ensure: %d requests", n)
	}
	// Within the probe interval: no network at all.
	idx, _, res, err = m.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	idx.Close()
	if res.Probed || res.Downloaded || s.requests.Load() != 1 {
		t.Errorf("second ensure: %+v requests=%d", res, s.requests.Load())
	}
	// Forced probe, same build: info.json only, no download.
	res, err = m.Refresh(ctx, true)
	if err != nil || !res.Probed || res.Downloaded || s.requests.Load() != 2 {
		t.Errorf("forced refresh: %+v %v requests=%d", res, err, s.requests.Load())
	}
	// A newer build appears: probe + download.
	s.db, s.builtAt, s.etag = withBuiltAt(t, "2026-10-17T03:17:00+00:00"), "2026-10-17T03:17:00+00:00", `"new"`
	res, err = m.Refresh(ctx, true)
	if err != nil || !res.Downloaded || res.BuiltAt.Format("2006-01-02") != "2026-10-17" {
		t.Fatalf("update: %+v %v", res, err)
	}
	if st := m.readState(); st.ETag != `"new"` || st.BuiltAt.Format("2006-01-02") != "2026-10-17" {
		t.Errorf("state %+v", st)
	}
	if age, stale := m.Stale(meta, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)); !stale || age < 9*24*time.Hour {
		t.Errorf("stale: %v %v", age, stale)
	}
}

func TestOfflineAndFailures(t *testing.T) {
	ctx := context.Background()
	db, _ := os.ReadFile(mini)
	s := newServer(t, db, "2026-10-17T03:17:00+00:00")
	dir := t.TempDir()
	m := manager(t, s, dir)
	m.Offline = true
	if _, _, _, err := m.Ensure(ctx); err != ErrNoSnapshot {
		t.Fatalf("offline without file: %v", err)
	}
	m.Offline = false
	idx, _, _, err := m.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	idx.Close() // Windows can't delete or replace an open file
	// Server gone: Refresh keeps the file and reports a note instead of failing.
	s.Close()
	res, err := m.Refresh(ctx, true)
	if err != nil || res.Note == "" {
		t.Errorf("unreachable: %+v %v", res, err)
	}
	if _, err := os.Stat(m.Path()); err != nil {
		t.Error("database vanished")
	}
	// A corrupt download must not replace the good file.
	bad := newServer(t, bytes.Repeat([]byte("x"), 4096), "2027-01-01T00:00:00+00:00")
	m2 := manager(t, bad, dir)
	res, err = m2.Refresh(ctx, true)
	if err != nil || res.Downloaded || res.Note == "" {
		t.Errorf("corrupt: %+v %v", res, err)
	}
	if idx, _, _, err := Open(m.Path()); err != nil {
		t.Errorf("good file damaged: %v", err)
	} else {
		idx.Close()
	}
	if _, err := os.Stat(m.Path() + ".download"); err == nil {
		t.Error("temp file left behind")
	}
}
