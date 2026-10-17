package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/culmat/luanthetti/internal/fsutil"
	"github.com/culmat/luanthetti/internal/library"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/luanticonf"
	"github.com/culmat/luanthetti/internal/store"
)

// Scan classifies every top-level entry in an installation's mods/, games/ and textures/:
// managed, adoptable (has author and release in its conf), git (a checkout), unmanaged,
// foreign_link, broken_link, or replaced (a real folder where Luanthetti placed a link).
func (a *App) Scan(ctx context.Context, name string) (*ScanReport, error) {
	insts, err := a.selectInstalls(nonEmpty(name))
	if err != nil {
		return nil, err
	}
	inst := insts[0]
	results, err := a.scan(inst)
	if err != nil {
		return nil, err
	}
	if err := a.Store.ReplaceScanResults(inst.ID, results); err != nil {
		return nil, err
	}
	rep := &ScanReport{Installation: inst.Name, Results: results, Counts: map[string]int{}}
	for _, r := range results {
		rep.Counts[r.Status]++
	}
	return rep, nil
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func (a *App) scan(inst store.Installation) ([]store.ScanResult, error) {
	links, err := a.Store.LinksIn(inst.ID)
	if err != nil {
		return nil, err
	}
	byPath := map[string]store.Link{}
	for _, l := range links {
		byPath[filepath.Clean(l.Path)] = l
	}
	results := []store.ScanResult{}
	for _, sub := range []string{"mods", "games", "textures"} {
		dir := filepath.Join(inst.Root, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") || (sub == "textures" && e.Name() == "base") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if strings.HasSuffix(path, ".luanthetti-new") || strings.HasSuffix(path, ".luanthetti-old") {
				continue
			}
			r := a.classify(path, byPath)
			if r.Status == "" {
				continue
			}
			results = append(results, r)
		}
	}
	return results, nil
}

func (a *App) classify(path string, byPath map[string]store.Link) store.ScanResult {
	r := store.ScanResult{Path: path}
	in, err := linker.Inspect(path)
	if err != nil || !in.Exists {
		return r
	}
	l, recorded := byPath[filepath.Clean(path)]
	if in.IsLink {
		switch {
		case in.Dangling:
			r.Status, r.Detail = "broken_link", "points at "+in.Target
		case recorded:
			r.Status = "managed"
		case in.PointsInto(a.Library.Root):
			r.Status, r.Detail = "foreign_link", "links into the library but is not recorded (another Luanthetti home?)"
		default:
			r.Status, r.Detail = "unmanaged", "a link to "+in.Target
		}
		r.FolderType = luanticonf.FolderType(path)
		return r
	}
	if !in.IsDir {
		return r
	}
	r.FolderType = luanticonf.FolderType(path)
	if r.FolderType == "" {
		return store.ScanResult{} // not content (stray folder)
	}
	conf := luanticonf.ReadConf(filepath.Join(path, luanticonf.ConfFile(r.FolderType)))
	r.Author = conf["author"]
	r.Name = conf["name"]
	if r.FolderType == "game" || r.Name == "" {
		r.Name = filepath.Base(path)
		if r.FolderType == "game" {
			r.Name = strings.TrimSuffix(r.Name, "_game")
		}
	}
	r.ReleaseID, _ = strconv.ParseInt(conf["release"], 10, 64)
	switch {
	case recorded && l.Kind == linker.Copy:
		r.Status = "managed"
	case recorded:
		r.Status, r.Detail = "replaced", fmt.Sprintf("a real folder (release %d) where a link was placed; another tool updated it", r.ReleaseID)
	case isDir(filepath.Join(path, ".git")) && r.ReleaseID == 0:
		r.Status, r.Detail = "git", "a git checkout; left alone"
	case r.Author != "" && r.ReleaseID > 0:
		r.Status = "adoptable"
	default:
		r.Status, r.Detail = "unmanaged", "no author/release in its conf (not installed from ContentDB)"
	}
	return r
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// AdoptRequest is `adopt`.
type AdoptRequest struct {
	Installation string
	Specs        []string // folder names or author/name; all adoptable when empty
	Copy         bool     // keep the folder in place and register it as a copy link
	DryRun       bool
	Force        bool // also adopt replaced folders and git checkouts with a release
}

// Adopt brings adoptable (and, with Force, replaced) folders under management: the folder is
// moved into the library as a version and replaced by a link (or registered as a copy).
func (a *App) Adopt(ctx context.Context, req AdoptRequest) (*AdoptReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	insts, err := a.selectInstalls(nonEmpty(req.Installation))
	if err != nil {
		return nil, err
	}
	rep := &AdoptReport{DryRun: req.DryRun, Adopted: []AdoptItem{}, Skipped: []AdoptItem{}}
	want := map[string]bool{}
	for _, s := range req.Specs {
		want[strings.ToLower(s)] = true
	}
	idx, idxErr := a.Deps(ctx)
	for _, inst := range insts {
		results, err := a.scan(inst)
		if err != nil {
			return rep, err
		}
		for _, r := range results {
			key := store.Key(r.Author, r.Name)
			if len(want) > 0 && !want[strings.ToLower(filepath.Base(r.Path))] && !want[key] && !want[strings.ToLower(r.Name)] {
				continue
			}
			item := AdoptItem{Installation: inst.Name, Path: r.Path, Package: r.Author + "/" + r.Name, Release: r.ReleaseID}
			ok := r.Status == "adoptable" || (req.Force && (r.Status == "replaced" || (r.Status == "git" && r.ReleaseID > 0)))
			if !ok {
				if len(want) > 0 || r.Status != "managed" {
					item.Action, item.Note = "skip", r.Status+": "+r.Detail
					rep.Skipped = append(rep.Skipped, item)
				}
				continue
			}
			typ := map[string]string{"mod": "mod", "modpack": "mod", "game": "game", "txp": "txp"}[r.FolderType]
			author, name := r.Author, r.Name
			if idxErr == nil {
				// Use ContentDB's spelling of the author and the package name.
				if p, err := idx.Package(author + "/" + name); err == nil {
					author, name, typ = p.Author, p.Name, p.Type
					item.Package = p.Key()
				}
			}
			if !luanticonf.ModNameRE.MatchString(name) {
				item.Action, item.Note = "skip", fmt.Sprintf("%q is not a valid package name", name)
				rep.Skipped = append(rep.Skipped, item)
				continue
			}
			if req.DryRun {
				item.Action = "moved"
				if req.Copy {
					item.Action = "copy_registered"
				}
				rep.Adopted = append(rep.Adopted, item)
				continue
			}
			if err := a.adoptOne(inst, r, library.Release{Author: author, Name: name, Type: typ, ReleaseID: r.ReleaseID}, req.Copy, &item); err != nil {
				item.Action, item.Note = "skip", err.Error()
				rep.Skipped = append(rep.Skipped, item)
				continue
			}
			rep.Adopted = append(rep.Adopted, item)
		}
	}
	return rep, nil
}

func (a *App) adoptOne(inst store.Installation, r store.ScanResult, rel library.Release, copyMode bool, item *AdoptItem) error {
	// A replaced link's record must go first; the folder becomes the new truth.
	if old, err := a.Store.LinkAt(inst.ID, r.Path); err == nil {
		if err := a.Store.RemoveLink(old.ID); err != nil {
			return err
		}
	}
	src := r.Path
	if copyMode {
		tmp, err := os.MkdirTemp(a.Dirs.Data, ".adopt-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		src = filepath.Join(tmp, filepath.Base(r.Path))
		if err := fsutil.CopyDir(r.Path, src); err != nil {
			return err
		}
	}
	pkg, ver, moved, err := a.Library.Adopt(src, rel, r.FolderType)
	if err != nil {
		return err
	}
	kind := linker.Copy
	switch {
	case copyMode:
		item.Action = "copy_registered"
	case !moved:
		// The library already had this release: keep the installation's folder in the trash.
		if err := a.toTrash(r.Path, "adopted-duplicate"); err != nil {
			return err
		}
		item.Action = "linked_existing"
	default:
		item.Action = "moved"
	}
	if !copyMode {
		kind, err = linker.Link(ver.Path, r.Path, linkMode(inst))
		if err != nil {
			return fmt.Errorf("linking %s: %w (the folder is now in the library at %s)", r.Path, err, ver.Path)
		}
	}
	_, err = a.Store.AddLink(store.Link{InstallationID: inst.ID, PackageID: pkg.ID, VersionID: ver.ID, Path: r.Path, Kind: kind, Reason: "adopted"})
	return err
}
