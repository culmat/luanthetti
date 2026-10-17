package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/installs"
	"github.com/culmat/luanthetti/internal/library"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/luanticonf"
	"github.com/culmat/luanthetti/internal/store"
)

// InstallRequest is `install`.
type InstallRequest struct {
	Specs   []string // author/name, a bare package name, or a local directory/zip
	Into    []string // installation names; all when empty
	NoDeps  bool
	Force   bool  // install even where no game can run it, and over unmanaged folders
	Release int64 // a specific release (only with one ContentDB spec)
	DryRun  bool
	// Confirm is asked once when dependencies would be installed; nil means "yes".
	Confirm func(rep *InstallReport) (bool, error)
}

// ErrDeclined is returned when Confirm says no.
var ErrDeclined = errors.New("cancelled, nothing changed")

type target struct {
	pkg   *depgraph.Pkg // nil for local packages
	local string        // local path
}

// Install places packages (and their hard dependencies) into installations.
func (a *App) Install(ctx context.Context, req InstallRequest) (*InstallReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(req.Specs) == 0 {
		return nil, errors.New("nothing to install")
	}
	insts, err := a.selectInstalls(req.Into)
	if err != nil {
		return nil, err
	}
	idx, err := a.Deps(ctx)
	if err != nil {
		return nil, err
	}
	var targets []target
	for _, spec := range req.Specs {
		t, err := a.resolveSpec(idx, spec)
		if err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	if req.Release > 0 && len(targets) != 1 {
		return nil, errors.New("--release needs exactly one package")
	}

	rep := &InstallReport{DryRun: req.DryRun}
	plans := make([][]PlanItem, len(insts))
	newDeps := false
	for i, inst := range insts {
		res, plan := a.planInstall(ctx, idx, inst, targets, req)
		rep.Results = append(rep.Results, res)
		plans[i] = plan
		for _, p := range plan {
			if p.Reason == "dependency" && p.Status == "planned" {
				newDeps = true
			}
		}
	}
	if req.DryRun {
		return rep, nil
	}
	if newDeps && req.Confirm != nil {
		ok, err := req.Confirm(rep)
		if err != nil {
			return rep, err
		}
		if !ok {
			return rep, ErrDeclined
		}
	}
	failed := 0
	for i, inst := range insts {
		res := &rep.Results[i]
		if res.Skipped != "" {
			continue
		}
		for j := range res.Items {
			item := &res.Items[j]
			if item.Status != "planned" {
				continue
			}
			if err := a.place(ctx, idx, inst, item, targets); err != nil {
				item.Status, item.Note = "failed", err.Error()
				failed++
				continue
			}
			item.Status = "installed"
		}
	}
	if failed > 0 {
		return rep, fmt.Errorf("%d package(s) failed", failed)
	}
	return rep, nil
}

// resolveSpec turns a spec into a snapshot package or a local path.
func (a *App) resolveSpec(idx *depgraph.Index, spec string) (target, error) {
	if depgraph.IsLocal(spec) || strings.HasSuffix(strings.ToLower(spec), ".zip") {
		if _, err := os.Stat(spec); err == nil {
			abs, _ := filepath.Abs(spec)
			return target{local: abs}, nil
		}
	}
	p, err := idx.Package(spec)
	if err != nil {
		return target{}, err
	}
	return target{pkg: p}, nil
}

// planInstall works out what goes into one installation.
func (a *App) planInstall(ctx context.Context, idx *depgraph.Index, inst store.Installation, targets []target, req InstallRequest) (InstallResult, []PlanItem) {
	res := InstallResult{Installation: inst.Name, Games: []GameStatus{}, Items: []PlanItem{}, Warnings: []string{}}
	if !installs.LooksLikeInstallation(inst.Root) {
		res.Skipped = "the installation directory is missing: " + inst.Root
		return res, nil
	}
	games, unknown := a.gamesOf(idx, inst)
	for _, g := range unknown {
		res.Warnings = append(res.Warnings, fmt.Sprintf("game %q is not on ContentDB; its compatibility is unknown", g.ID))
	}
	present := a.presentPackages(inst)
	planned := map[string]bool{}
	add := func(item PlanItem) {
		key := strings.ToLower(item.Package)
		if planned[key] {
			for i := range res.Items {
				if strings.EqualFold(res.Items[i].Package, item.Package) && item.Reason == "explicit" {
					res.Items[i].Reason = "explicit"
				} else if strings.EqualFold(res.Items[i].Package, item.Package) {
					res.Items[i].RequiredBy = appendUnique(res.Items[i].RequiredBy, item.RequiredBy...)
				}
			}
			return
		}
		planned[key] = true
		res.Items = append(res.Items, item)
	}
	for _, t := range targets {
		if t.local != "" {
			add(PlanItem{Package: "local/" + filepath.Base(t.local), Reason: "explicit", Status: "planned", Note: t.local})
			continue
		}
		p := t.pkg
		if p.Type == "game" && len(targets) > 0 {
			// Games are installed, not resolved against games.
			add(a.planItem(ctx, idx, inst, p, "explicit", nil, req.Release, present))
			continue
		}
		deps := map[string][]string{} // dependency key → required by
		anyOK := len(games) == 0
		for _, g := range games {
			gs, resolution := a.gameStatus(idx, p, g)
			res.Games = append(res.Games, gs)
			if gs.Status == "unsatisfiable" {
				continue
			}
			anyOK = true
			for _, e := range resolution.Install {
				deps[e.Package] = appendUnique(deps[e.Package], e.NeededBy)
			}
		}
		if len(games) == 0 {
			res.Warnings = append(res.Warnings, "no known games in this installation; dependencies were not resolved")
		}
		if !anyOK && !req.Force {
			res.Skipped = fmt.Sprintf("%s cannot run in any game of this installation (use --force to install anyway)", p.Key())
			res.Items = []PlanItem{}
			return res, nil
		}
		add(a.planItem(ctx, idx, inst, p, "explicit", nil, req.Release, present))
		if req.NoDeps {
			continue
		}
		for _, key := range sortedKeys(deps) {
			dp, err := idx.Package(key)
			if err != nil {
				res.Warnings = append(res.Warnings, err.Error())
				continue
			}
			add(a.planItem(ctx, idx, inst, dp, "dependency", deps[key], 0, present))
		}
	}
	a.checkConflicts(idx, inst, &res, present, req.Force)
	dropOrphanedDeps(&res)
	return res, res.Items
}

// dropOrphanedDeps skips dependencies whose every requirer is skipped, so a refused package
// doesn't leave its dependencies behind.
func dropOrphanedDeps(res *InstallResult) {
	alive := func(key string) bool {
		for _, it := range res.Items {
			if strings.EqualFold(it.Package, key) {
				return it.Status != "skipped" && it.Status != "failed"
			}
		}
		return true // required by something already present or outside the plan
	}
	for changed := true; changed; {
		changed = false
		for i := range res.Items {
			it := &res.Items[i]
			if it.Reason != "dependency" || it.Status != "planned" || len(it.RequiredBy) == 0 {
				continue
			}
			needed := false
			for _, r := range it.RequiredBy {
				if alive(r) {
					needed = true
					break
				}
			}
			if !needed {
				it.Status, it.Note = "skipped", "not needed: "+strings.Join(it.RequiredBy, ", ")+" was skipped"
				changed = true
			}
		}
	}
}

// gameStatus is the compat status and resolution of a package in one game.
func (a *App) gameStatus(idx *depgraph.Index, p *depgraph.Pkg, g GameRef) (GameStatus, *depgraph.Resolution) {
	row := idx.CompatIn(p, g.pkg)
	res := idx.ResolveIn(p, g.pkg)
	return GameStatus{Game: g.Key, Title: g.Title, Status: row.Status, Missing: row.Missing, Install: row.Install, Risky: row.UndeclaredMissing}, res
}

// presentPackages maps lower(author)/name of every package linked in an installation to its link.
func (a *App) presentPackages(inst store.Installation) map[string]store.Link {
	out := map[string]store.Link{}
	links, _ := a.Store.LinksIn(inst.ID)
	for _, l := range links {
		if pkg, err := a.Store.PackageByID(l.PackageID); err == nil {
			out[pkg.Key] = l
		}
	}
	return out
}

func (a *App) planItem(ctx context.Context, idx *depgraph.Index, inst store.Installation, p *depgraph.Pkg, reason string, requiredBy []string, release int64, present map[string]store.Link) PlanItem {
	folderType := map[string]string{"mod": "mod", "game": "game", "txp": "txp"}[p.Type]
	item := PlanItem{Package: p.Key(), Type: p.Type, Folder: p.Name, Reason: reason, RequiredBy: requiredBy, Status: "planned",
		Path: filepath.Join(installs.ContentDir(inst.Root, folderType), p.Name)}
	if l, ok := present[store.Key(p.Author, p.Name)]; ok {
		item.Status, item.Path, item.LinkKind = "present", l.Path, l.Kind
		if v, err := a.Store.VersionByID(l.VersionID); err == nil {
			item.Release = v.ReleaseID
		}
		return item
	}
	rel, err := a.releaseFor(ctx, inst, p, idx, release)
	if err != nil {
		item.Status, item.Note = "skipped", err.Error()
		return item
	}
	item.Release = rel
	return item
}

// checkConflicts refuses to place a package where something unmanaged already uses its
// folder or one of its mod names (two mods with one name in mods/ stop the engine).
func (a *App) checkConflicts(idx *depgraph.Index, inst store.Installation, res *InstallResult, present map[string]store.Link, force bool) {
	existing := map[string]string{} // mod name → path, for unmanaged content in mods/
	managed := map[string]bool{}
	for _, l := range present {
		managed[filepath.Clean(l.Path)] = true
	}
	modsDir := filepath.Join(inst.Root, "mods")
	for _, m := range luanticonf.CollectMods(modsDir, modsDir) {
		top := filepath.Join(modsDir, strings.SplitN(filepath.ToSlash(m.Path), "/", 2)[0])
		if !managed[filepath.Clean(top)] {
			existing[m.Name] = top
		}
	}
	for i := range res.Items {
		item := &res.Items[i]
		if item.Status != "planned" || strings.HasPrefix(item.Package, "local/") {
			continue
		}
		if in, _ := linker.Inspect(item.Path); in.Exists {
			if force {
				item.Note = "replacing the unmanaged " + item.Path + " (moved to the trash)"
				continue
			}
			item.Status, item.Note = "skipped", item.Path+" already exists and is not managed by luanthetti (see `luanthetti scan`/`adopt`, or --force)"
			continue
		}
		if p, err := idx.Package(item.Package); err == nil && item.Type == "mod" {
			for _, name := range p.Provides.Sorted() {
				if path, ok := existing[name]; ok {
					if force {
						res.Warnings = append(res.Warnings, fmt.Sprintf("mod name %q is also provided by %s; the engine refuses two mods with one name in mods/", name, path))
						continue
					}
					item.Status, item.Note = "skipped", fmt.Sprintf("mod %q already exists at %s (unmanaged); two mods with one name in mods/ stop the engine", name, path)
					break
				}
			}
		}
	}
}

// place downloads (if needed) and links one plan item.
func (a *App) place(ctx context.Context, idx *depgraph.Index, inst store.Installation, item *PlanItem, targets []target) error {
	var pkg store.Package
	var ver store.Version
	var err error
	if strings.HasPrefix(item.Package, "local/") {
		pkg, ver, err = a.Library.AddLocal(item.Note)
		if err != nil {
			return err
		}
		item.Package, item.Folder, item.Note = "local/"+pkg.Name, pkg.Folder, ""
		item.Path = filepath.Join(installs.ContentDir(inst.Root, pkg.FolderType), pkg.Folder)
		if in, _ := linker.Inspect(item.Path); in.Exists {
			return fmt.Errorf("%s already exists", item.Path)
		}
	} else {
		p, err := idx.Package(item.Package)
		if err != nil {
			return err
		}
		d := idx.Details(p)
		pkg, ver, err = a.Library.Ensure(ctx, library.Release{Author: p.Author, Name: p.Name, Type: p.Type, Title: p.Title,
			ShortDescription: d.ShortDescription, ReleaseID: item.Release})
		if err != nil {
			return err
		}
		item.Path = filepath.Join(installs.ContentDir(inst.Root, pkg.FolderType), pkg.Folder)
	}
	if in, _ := linker.Inspect(item.Path); in.Exists {
		// --force over an unmanaged folder: keep it in the trash rather than deleting it.
		if err := a.toTrash(item.Path, "replaced-by-install"); err != nil {
			return err
		}
	}
	kind, err := linker.Link(ver.Path, item.Path, linkMode(inst))
	if err != nil {
		return err
	}
	item.LinkKind = kind
	_, err = a.Store.AddLink(store.Link{InstallationID: inst.ID, PackageID: pkg.ID, VersionID: ver.ID, Path: item.Path,
		Kind: kind, Reason: item.Reason, RequiredBy: item.RequiredBy})
	return err
}

// toTrash moves a directory into the trash.
func (a *App) toTrash(path, reason string) error {
	dest := filepath.Join(a.Dirs.TrashDir(), fmt.Sprintf("%s-%s-%s", nowStamp(), reason, filepath.Base(path)))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return moveDir(path, dest)
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		found := false
		for _, x := range list {
			if x == it {
				found = true
				break
			}
		}
		if !found && it != "" {
			list = append(list, it)
		}
	}
	sort.Strings(list)
	return list
}
