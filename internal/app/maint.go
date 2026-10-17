package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/installs"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/snapshot"
	"github.com/culmat/luanthetti/internal/version"
)

// RefreshReport is `refresh`.
type RefreshReport struct {
	Snapshot snapshot.Result `json:"snapshot"`
	Meta     depgraph.Meta   `json:"meta"`
	Updates  map[string]int  `json:"updates"` // installation → packages in the update list
	Notes    []string        `json:"notes"`
}

// Refresh downloads a newer dependency snapshot if one exists and refreshes the update lists.
func (a *App) Refresh(ctx context.Context) (*RefreshReport, error) {
	rep := &RefreshReport{Updates: map[string]int{}, Notes: []string{}}
	if a.snapshotFile == "" {
		res, err := a.Snap.Refresh(ctx, true)
		if err != nil {
			return nil, err
		}
		rep.Snapshot = res
		if res.Note != "" {
			rep.Notes = append(rep.Notes, res.Note)
		}
		a.reloadDeps()
	}
	meta, err := a.SnapshotMeta(ctx)
	if err != nil {
		return nil, err
	}
	rep.Meta = meta
	insts, _ := a.Store.Installations()
	for _, inst := range insts {
		m, err := a.latestReleases(ctx, inst.EngineVersion, inst.Protocol, true)
		if err != nil {
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s: %v", inst.Name, err))
			continue
		}
		rep.Updates[inst.Name] = len(m)
	}
	return rep, nil
}

// GCReport is `gc`.
type GCReport struct {
	Versions []string `json:"versions"`
	Trash    []string `json:"trash"`
	Partial  []string `json:"partial"`
	DryRun   bool     `json:"dry_run"`
}

// GC removes unreferenced library versions beyond keep_versions, incomplete downloads and
// trash older than seven days.
func (a *App) GC(ctx context.Context, dryRun bool) (*GCReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rep := &GCReport{Versions: []string{}, Trash: []string{}, Partial: []string{}, DryRun: dryRun}
	pkgs, err := a.Store.Packages()
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		if dryRun {
			continue
		}
		removed, err := a.Library.Prune(p.ID, a.Config.Library.KeepVersions)
		if err != nil {
			return rep, err
		}
		for _, v := range removed {
			rep.Versions = append(rep.Versions, fmt.Sprintf("%s/%s release %d", p.Author, p.Name, v.ReleaseID))
		}
	}
	// Release directories without .complete are interrupted downloads.
	matches, _ := filepath.Glob(filepath.Join(a.Library.Root, "*", "*", "*", "*"))
	for _, dir := range matches {
		if _, err := os.Stat(filepath.Join(dir, ".complete")); err == nil {
			continue
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() || time.Since(st.ModTime()) < time.Hour {
			continue
		}
		rep.Partial = append(rep.Partial, dir)
		if !dryRun {
			os.RemoveAll(dir)
		}
	}
	entries, _ := os.ReadDir(a.Dirs.TrashDir())
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < 7*24*time.Hour {
			continue
		}
		p := filepath.Join(a.Dirs.TrashDir(), e.Name())
		rep.Trash = append(rep.Trash, p)
		if !dryRun {
			os.RemoveAll(p)
		}
	}
	return rep, nil
}

// EnvReport is `env`: how to start a server without links.
type EnvReport struct {
	Installation string   `json:"installation"`
	ModPaths     []string `json:"mod_paths"`
	Env          string   `json:"env"`
	WorldLines   []string `json:"world_mt_lines"`
}

// Env prints LUANTI_MOD_PATH for the library folders of an installation's mods, and the
// world.mt lines that enable them by absolute path (doc/world_format.md "load_mod_").
func (a *App) Env(ctx context.Context, name string) (*EnvReport, error) {
	insts, err := a.selectInstalls(nonEmpty(name))
	if err != nil {
		return nil, err
	}
	inst := insts[0]
	rep := &EnvReport{Installation: inst.Name, ModPaths: []string{}, WorldLines: []string{}}
	links, _ := a.Store.LinksIn(inst.ID)
	seen := map[string]bool{}
	for _, l := range links {
		p, _ := a.Store.PackageByID(l.PackageID)
		if p.FolderType != "mod" && p.FolderType != "modpack" {
			continue
		}
		v, err := a.Store.VersionByID(l.VersionID)
		if err != nil {
			continue
		}
		parent := filepath.Dir(v.Path)
		if !seen[parent] {
			seen[parent] = true
			rep.ModPaths = append(rep.ModPaths, parent)
		}
		rep.WorldLines = append(rep.WorldLines, fmt.Sprintf("load_mod_%s = %s", p.Folder, v.Path))
	}
	sort.Strings(rep.WorldLines)
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	rep.Env = "LUANTI_MOD_PATH=" + strings.Join(rep.ModPaths, sep)
	return rep, nil
}

// DoctorReport is `doctor`.
type DoctorReport struct {
	Version       string            `json:"version"`
	Data          string            `json:"data_dir"`
	Config        string            `json:"config_file"`
	Cache         string            `json:"cache_dir"`
	Library       string            `json:"library_dir"`
	Snapshot      *depgraph.Meta    `json:"snapshot,omitempty"`
	SnapshotAge   string            `json:"snapshot_age,omitempty"`
	SnapshotError string            `json:"snapshot_error,omitempty"`
	Precomputed   bool              `json:"precomputed_answers"`
	RulesKnown    int               `json:"rules_version_known"`
	Installations []InstallView     `json:"installations"`
	LinkProbe     string            `json:"link_probe"`
	ContentDB     string            `json:"contentdb"`
	Binaries      []installs.Binary `json:"binaries"`
}

// Doctor reports paths, the snapshot, installations, link capability and ContentDB reachability.
func (a *App) Doctor(ctx context.Context) *DoctorReport {
	rep := &DoctorReport{Version: version.Version(), Data: a.Dirs.Data, Config: a.Dirs.ConfigFile(), Cache: a.Dirs.Cache,
		Library: a.Library.Root, RulesKnown: depgraph.KnownRulesVersion, ContentDB: a.CDB.Base}
	if idx, err := a.Deps(ctx); err != nil {
		rep.SnapshotError = err.Error()
	} else {
		m := a.meta
		rep.Snapshot = &m
		rep.SnapshotAge = time.Since(m.BuiltAt).Round(time.Hour).String()
		rep.Precomputed = idx.HasPrecomputed()
	}
	rep.Installations, _ = a.ListInstalls(ctx)
	rep.LinkProbe = linker.Probe(filepath.Join(a.Dirs.Data, "probe"))
	os.RemoveAll(filepath.Join(a.Dirs.Data, "probe"))
	rep.Binaries = installs.FindBinaries(ctx)
	if a.Offline {
		rep.ContentDB += " (offline mode)"
	} else if _, err := a.CDB.Tags(ctx); err != nil {
		rep.ContentDB += " (unreachable: " + err.Error() + ")"
	} else {
		rep.ContentDB += " (reachable)"
	}
	return rep
}
