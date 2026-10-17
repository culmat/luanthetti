// Package app is Luanthetti's use-case layer. The command line and the web UI both call it;
// it returns report structs (report.go) that are printed as text, as JSON with --json, and
// served by the web API unchanged. Nothing in this package prints.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/culmat/luanthetti/internal/config"
	"github.com/culmat/luanthetti/internal/contentdb"
	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/installs"
	"github.com/culmat/luanthetti/internal/library"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/paths"
	"github.com/culmat/luanthetti/internal/snapshot"
	"github.com/culmat/luanthetti/internal/store"
	"github.com/culmat/luanthetti/internal/version"
)

// Options configure New.
type Options struct {
	Home         string // --home; "" for LUANTHETTI_HOME or the per-OS default
	ContentDBURL string // overrides config
	SnapshotURL  string // overrides config and LUANTHETTI_SNAPSHOT_URL
	SnapshotFile string // use this database file as is (tests, LUANTHETTI_SNAPSHOT_FILE)
	Offline      bool
	HTTPClient   *http.Client // tests
	ContentDBRPS float64      // tests against a local fake server; never above 2 for content.luanti.org
	Log          func(format string, args ...any)
}

// App holds everything an operation needs.
type App struct {
	Dirs    paths.Dirs
	Config  config.Config
	Store   *store.Store
	Library *library.Library
	CDB     *contentdb.Client
	Snap    *snapshot.Manager
	Offline bool
	Log     func(format string, args ...any)

	snapshotFile string
	mu           sync.Mutex // one mutating operation at a time
	idxMu        sync.Mutex
	idx          *depgraph.Index
	meta         depgraph.Meta
}

// New opens the state and prepares the clients. The dependency snapshot is loaded lazily.
func New(opts Options) (*App, error) {
	dirs, err := paths.Resolve(opts.Home)
	if err != nil {
		return nil, err
	}
	if err := dirs.Ensure(); err != nil {
		return nil, err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dirs.StateFile())
	if err != nil {
		return nil, err
	}
	log := opts.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	cdbURL := firstNonEmpty(opts.ContentDBURL, os.Getenv("LUANTHETTI_CONTENTDB_URL"), cfg.ContentDB.URL)
	cdb := contentdb.New(cdbURL, version.UserAgent(), cfg.ContentDB.RequestsPerSecond)
	if opts.HTTPClient != nil {
		cdb.HTTP = opts.HTTPClient
	}
	if opts.ContentDBRPS > 0 {
		cdb.SetLimit(opts.ContentDBRPS)
	}
	libRoot := cfg.Library.Path
	if libRoot == "" {
		libRoot = dirs.LibraryDir()
	}
	a := &App{
		Dirs: dirs, Config: cfg, Store: st, CDB: cdb, Offline: opts.Offline, Log: log,
		Library:      &library.Library{Root: libRoot, Store: st, CDB: cdb, Log: log},
		snapshotFile: firstNonEmpty(opts.SnapshotFile, os.Getenv("LUANTHETTI_SNAPSHOT_FILE")),
	}
	a.Snap = &snapshot.Manager{
		Dir:           dirs.DepgraphDir(),
		URL:           firstNonEmpty(opts.SnapshotURL, os.Getenv("LUANTHETTI_SNAPSHOT_URL"), cfg.Depgraph.SnapshotURL),
		UserAgent:     version.UserAgent(),
		Client:        opts.HTTPClient,
		Offline:       opts.Offline,
		ProbeInterval: cfg.Depgraph.AutoRefresh.Duration,
		Log:           log,
	}
	if cfg.Depgraph.AutoRefresh.Duration == 0 {
		a.Snap.ProbeInterval = 1<<63 - 1
	}
	return a, nil
}

// Close releases the database handles.
func (a *App) Close() error {
	a.idxMu.Lock()
	if a.idx != nil {
		a.idx.Close()
		a.idx = nil
	}
	a.idxMu.Unlock()
	return a.Store.Close()
}

// Deps returns the dependency index, downloading or refreshing the snapshot as needed.
func (a *App) Deps(ctx context.Context) (*depgraph.Index, error) {
	a.idxMu.Lock()
	defer a.idxMu.Unlock()
	if a.idx != nil {
		return a.idx, nil
	}
	if a.snapshotFile != "" {
		idx, meta, warning, err := snapshot.Open(a.snapshotFile)
		if err != nil {
			return nil, err
		}
		if warning != "" {
			a.Log("%s", warning)
		}
		a.idx, a.meta = idx, meta
		return idx, nil
	}
	idx, meta, _, err := a.Snap.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	if age, stale := a.Snap.Stale(meta, time.Now()); stale {
		a.Log("dependency snapshot built %s (%d days old); run `luanthetti refresh`", meta.BuiltAt.Format("2006-01-02"), int(age.Hours()/24))
	}
	a.idx, a.meta = idx, meta
	return idx, nil
}

// SnapshotMeta returns the loaded snapshot's metadata (loading it if needed).
func (a *App) SnapshotMeta(ctx context.Context) (depgraph.Meta, error) {
	if _, err := a.Deps(ctx); err != nil {
		return depgraph.Meta{}, err
	}
	return a.meta, nil
}

// reloadDeps drops the index so the next Deps call opens the (new) snapshot.
func (a *App) reloadDeps() {
	a.idxMu.Lock()
	if a.idx != nil {
		a.idx.Close()
		a.idx = nil
	}
	a.idxMu.Unlock()
}

// selectInstalls returns the named installations, or all registered ones when names is empty.
func (a *App) selectInstalls(names []string) ([]store.Installation, error) {
	all, err := a.Store.Installations()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		if len(all) == 0 {
			return nil, errors.New("no installations registered; run `luanthetti installs detect --add` or `luanthetti installs add <path>`")
		}
		return all, nil
	}
	var out []store.Installation
	for _, n := range names {
		inst, err := a.Store.Installation(n)
		if err != nil {
			return nil, fmt.Errorf("no installation %q (see `luanthetti installs list`)", n)
		}
		out = append(out, inst)
	}
	return out, nil
}

// gamesOf maps an installation's game folders to snapshot packages. Games unknown to the
// snapshot are returned in unknown.
func (a *App) gamesOf(idx *depgraph.Index, inst store.Installation) (known []GameRef, unknown []GameRef) {
	for _, g := range installs.Games(inst.Root, inst.ExtraGameDirs...) {
		ref := GameRef{ID: g.ID, Title: g.Title}
		if p := idx.GameForInstalled(g.ID, g.Author, g.Title); p != nil {
			ref.Key, ref.pkg = p.Key(), p
			if ref.Title == "" {
				ref.Title = p.Title
			}
			known = append(known, ref)
		} else {
			unknown = append(unknown, ref)
		}
	}
	return known, unknown
}

// latestReleases returns /api/updates/ for an engine version, cached for updates_ttl.
func (a *App) latestReleases(ctx context.Context, engine string, protocol int, force bool) (map[string]int64, error) {
	key := fmt.Sprintf("updates:%s:%d", engine, protocol)
	if data, fetched, err := a.Store.CachedBlob(key); err == nil && !force &&
		(a.Offline || time.Since(fetched) < a.Config.ContentDB.UpdatesTTL.Duration) {
		var m map[string]int64
		if json.Unmarshal(data, &m) == nil {
			return m, nil
		}
	}
	if a.Offline {
		return nil, errors.New("offline and no cached update list")
	}
	m, err := a.CDB.LatestReleases(ctx, engine, protocol)
	if err != nil {
		// Fall back to a stale cache rather than failing.
		if data, _, cerr := a.Store.CachedBlob(key); cerr == nil {
			var cached map[string]int64
			if json.Unmarshal(data, &cached) == nil {
				a.Log("ContentDB unreachable (%v); using the cached update list", err)
				return cached, nil
			}
		}
		return nil, err
	}
	data, _ := json.Marshal(m)
	_ = a.Store.PutCachedBlob(key, data)
	return m, nil
}

// releaseFor picks the release to install for a package in an installation: an explicit
// release, the latest one compatible with the installation's engine (from /api/updates/), or
// the snapshot's latest release.
func (a *App) releaseFor(ctx context.Context, inst store.Installation, p *depgraph.Pkg, idx *depgraph.Index, explicit int64) (int64, error) {
	if explicit > 0 {
		return explicit, nil
	}
	m, err := a.latestReleases(ctx, inst.EngineVersion, inst.Protocol, false)
	if err == nil {
		key := store.Key(p.Author, p.Name)
		if rel, ok := m[key]; ok && rel > 0 {
			return rel, nil
		}
		if p.Type == "game" {
			if rel, ok := m[strings.TrimSuffix(key, "_game")]; ok && rel > 0 {
				return rel, nil
			}
		}
	}
	if d := idx.Details(p); d.ReleaseID > 0 {
		return d.ReleaseID, nil
	}
	if err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("%s has no release compatible with Luanti %s", p.Key(), firstNonEmpty(inst.EngineVersion, "(unknown version)"))
}

// linkMode is the mode to use for new links in an installation.
func linkMode(inst store.Installation) string {
	if inst.LinkMode == "" {
		return linker.Auto
	}
	return inst.LinkMode
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
