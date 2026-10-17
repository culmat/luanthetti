package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/culmat/luanthetti/internal/installs"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/store"
)

// RemoveRequest is `remove`.
type RemoveRequest struct {
	Specs       []string // author/name or bare package name (as linked)
	Into        []string
	KeepLibrary bool
	Force       bool // remove even when other packages need it
	DryRun      bool
}

// Remove unlinks packages from installations, then dependencies nothing needs any more, then
// library versions no installation links (unless KeepLibrary).
func (a *App) Remove(ctx context.Context, req RemoveRequest) (*RemoveReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	insts, err := a.selectInstalls(req.Into)
	if err != nil {
		return nil, err
	}
	rep := &RemoveReport{Removed: []RemovedLink{}, Library: []string{}, Warnings: []string{}}
	var pkgs []store.Package
	for _, spec := range req.Specs {
		p, err := a.findLinkedPackage(spec)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
	touched := map[int64]bool{}
	for _, inst := range insts {
		present := a.presentPackages(inst)
		for _, p := range pkgs {
			l, ok := present[p.Key]
			if !ok {
				continue
			}
			needers := a.neededBy(inst, p)
			if len(needers) > 0 && !req.Force {
				return rep, fmt.Errorf("%s is needed by %s in %s (use --force to remove it anyway)", p.Author+"/"+p.Name, strings.Join(needers, ", "), inst.Name)
			}
			if err := a.unlink(inst, l, p, req.DryRun, rep); err != nil {
				return rep, err
			}
			touched[p.ID] = true
			a.warnWorlds(inst, l, p, rep)
		}
		// Dependencies that nothing in this installation requires any more.
		for changed := true; changed; {
			changed = false
			links, _ := a.Store.LinksIn(inst.ID)
			for _, l := range links {
				if l.Reason != "dependency" {
					continue
				}
				p, err := a.Store.PackageByID(l.PackageID)
				if err != nil {
					continue
				}
				if len(a.neededBy(inst, p)) > 0 {
					continue
				}
				if req.DryRun && touched[p.ID] {
					continue
				}
				if err := a.unlink(inst, l, p, req.DryRun, rep); err != nil {
					return rep, err
				}
				touched[p.ID] = true
				changed = !req.DryRun
			}
		}
	}
	if len(rep.Removed) == 0 {
		return rep, errors.New("none of the named packages is linked in the selected installations")
	}
	if req.KeepLibrary || req.DryRun {
		return rep, nil
	}
	for id := range touched {
		links, _ := a.Store.LinksOf(id)
		if len(links) > 0 {
			continue
		}
		p, _ := a.Store.PackageByID(id)
		versions, _ := a.Store.VersionsOf(id)
		for _, v := range versions {
			if err := a.Library.RemoveVersion(v); err != nil {
				rep.Warnings = append(rep.Warnings, err.Error())
			}
		}
		if err := a.Store.DeletePackage(id); err == nil {
			rep.Library = append(rep.Library, p.Author+"/"+p.Name)
		}
	}
	return rep, nil
}

func (a *App) unlink(inst store.Installation, l store.Link, p store.Package, dryRun bool, rep *RemoveReport) error {
	rep.Removed = append(rep.Removed, RemovedLink{Installation: inst.Name, Package: p.Author + "/" + p.Name, Path: l.Path, Reason: l.Reason})
	if dryRun {
		return nil
	}
	if err := linker.Unlink(inst.Root, l.Path, l.Kind); err != nil {
		return fmt.Errorf("%s: %w", l.Path, err)
	}
	return a.Store.RemoveLink(l.ID)
}

// neededBy lists the packages linked in an installation that list p in required_by terms:
// links whose package is in p's link required_by and still present.
func (a *App) neededBy(inst store.Installation, p store.Package) []string {
	present := a.presentPackages(inst)
	l, ok := present[p.Key]
	if !ok {
		return nil
	}
	var out []string
	for _, req := range l.RequiredBy {
		author, name, _ := strings.Cut(req, "/")
		if _, still := present[store.Key(author, name)]; still {
			out = append(out, req)
		}
	}
	return out
}

// findLinkedPackage finds a library package by author/name (any case) or by bare name.
func (a *App) findLinkedPackage(spec string) (store.Package, error) {
	spec = strings.Trim(strings.TrimSpace(spec), "/")
	if strings.Contains(spec, "/") {
		author, name, _ := strings.Cut(spec, "/")
		p, err := a.Store.PackageByKey(store.Key(author, name))
		if err != nil {
			return store.Package{}, fmt.Errorf("%s is not in the library", spec)
		}
		return p, nil
	}
	all, err := a.Store.Packages()
	if err != nil {
		return store.Package{}, err
	}
	var matches []store.Package
	for _, p := range all {
		if p.Name == spec || p.Folder == spec {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return store.Package{}, fmt.Errorf("%s is not in the library", spec)
	case 1:
		return matches[0], nil
	}
	var keys []string
	for _, m := range matches {
		keys = append(keys, m.Author+"/"+m.Name)
	}
	return store.Package{}, fmt.Errorf("%s is ambiguous: %s", spec, strings.Join(keys, ", "))
}

// warnWorlds notes worlds that still enable the removed package's mods.
func (a *App) warnWorlds(inst store.Installation, l store.Link, p store.Package, rep *RemoveReport) {
	names := map[string]bool{p.Folder: true, p.Name: true}
	for _, w := range installs.Worlds(inst.Root) {
		for name := range names {
			if w.MT.Enabled(name) {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("world %q in %s still enables %s (load_mod_%s); Luanti will report it missing", w.Name, inst.Name, name, name))
			}
		}
	}
}
