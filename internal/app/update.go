package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/library"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/store"
)

// UpdateRequest is `update`.
type UpdateRequest struct {
	Specs   []string // limit to these packages; all when empty
	Into    []string
	Exclude []string
	Check   bool // only report what is outdated
	DryRun  bool
	NoDeps  bool
	Confirm func(rep *UpdateReport) (bool, error)
}

// Update compares each linked package's release with ContentDB's /api/updates/ (as the
// in-game update detector does), downloads newer releases into the library, switches the
// links, installs newly needed dependencies and prunes old versions.
func (a *App) Update(ctx context.Context, req UpdateRequest) (*UpdateReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	insts, err := a.selectInstalls(req.Into)
	if err != nil {
		return nil, err
	}
	idx, err := a.Deps(ctx)
	if err != nil {
		return nil, err
	}
	only := map[string]bool{}
	for _, s := range req.Specs {
		p, err := a.findLinkedPackage(s)
		if err != nil {
			return nil, err
		}
		only[p.Key] = true
	}
	exclude := map[string]bool{}
	for _, x := range req.Exclude {
		exclude[strings.ToLower(strings.TrimSpace(x))] = true
	}
	rep := &UpdateReport{Check: req.Check, DryRun: req.DryRun, Items: []UpdateItem{}, Pruned: []string{}, Warnings: []string{}}
	type job struct {
		inst store.Installation
		link store.Link
		pkg  store.Package
		item int
	}
	var jobs []job
	newDeps := false
	for _, inst := range insts {
		latest, err := a.latestReleases(ctx, inst.EngineVersion, inst.Protocol, false)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: cannot check for updates: %v", inst.Name, err))
			continue
		}
		if inst.EngineVersion == "" {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: Luanti version unknown, so releases are not filtered by engine version", inst.Name))
		}
		links, _ := a.Store.LinksIn(inst.ID)
		for _, l := range links {
			p, err := a.Store.PackageByID(l.PackageID)
			if err != nil || p.Source != "contentdb" {
				continue
			}
			if len(only) > 0 && !only[p.Key] {
				continue
			}
			v, _ := a.Store.VersionByID(l.VersionID)
			rel, ok := latest[p.Key]
			if !ok && p.Type == "game" {
				rel, ok = latest[strings.TrimSuffix(p.Key, "_game")]
			}
			if !ok || rel <= v.ReleaseID {
				continue
			}
			item := UpdateItem{Installation: inst.Name, Package: p.Author + "/" + p.Name, Folder: p.Folder, Current: v.ReleaseID, Latest: rel, Status: "outdated"}
			if exclude[strings.ToLower(p.Name)] || exclude[p.Key] || exclude[strings.ToLower(p.Folder)] {
				item.Status = "excluded"
			} else if pin, _ := a.Store.Pin(inst.ID, p.ID); pin != nil {
				item.Status, item.Note = "pinned", "pinned"
				if pin.ReleaseID != nil {
					item.Note = fmt.Sprintf("pinned to release %d", *pin.ReleaseID)
				}
			} else if dp, err := idx.Package(p.Author + "/" + p.Name); err == nil && dp.Type != "game" {
				games, _ := a.gamesOf(idx, inst)
				present := a.presentPackages(inst)
				for _, g := range games {
					gs, res := a.gameStatus(idx, dp, g)
					item.Games = append(item.Games, gs)
					if gs.Status == "unsatisfiable" {
						continue
					}
					for _, e := range res.Install {
						if ep, err := idx.Package(e.Package); err == nil {
							if _, have := present[store.Key(ep.Author, ep.Name)]; !have {
								item.NewDeps = appendUnique(item.NewDeps, ep.Key())
							}
						}
					}
				}
				if len(item.NewDeps) > 0 && !req.NoDeps {
					newDeps = true
				}
			}
			rep.Items = append(rep.Items, item)
			if item.Status == "outdated" {
				jobs = append(jobs, job{inst: inst, link: l, pkg: p, item: len(rep.Items) - 1})
			}
		}
	}
	if req.Check || req.DryRun {
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
	touched := map[int64]bool{}
	for _, j := range jobs {
		item := &rep.Items[j.item]
		if err := a.updateLink(ctx, idx, j.inst, j.link, j.pkg, item.Latest); err != nil {
			item.Status, item.Note = "failed", err.Error()
			failed++
			continue
		}
		item.Status = "updated"
		touched[j.pkg.ID] = true
		if len(item.NewDeps) > 0 && !req.NoDeps {
			if err := a.installDeps(ctx, idx, j.inst, item.NewDeps, item.Package); err != nil {
				item.Note = "new dependencies: " + err.Error()
				failed++
			}
		}
	}
	for id := range touched {
		removed, err := a.Library.Prune(id, a.Config.Library.KeepVersions)
		if err != nil {
			rep.Warnings = append(rep.Warnings, err.Error())
		}
		p, _ := a.Store.PackageByID(id)
		for _, v := range removed {
			rep.Pruned = append(rep.Pruned, fmt.Sprintf("%s/%s release %d", p.Author, p.Name, v.ReleaseID))
		}
	}
	if failed > 0 {
		return rep, fmt.Errorf("%d update(s) failed", failed)
	}
	return rep, nil
}

// updateLink downloads a release and switches the link to it.
func (a *App) updateLink(ctx context.Context, idx *depgraph.Index, inst store.Installation, l store.Link, p store.Package, release int64) error {
	r := library.Release{Author: p.Author, Name: p.Name, Type: p.Type, Title: p.Title, ShortDescription: p.ShortDescription, ReleaseID: release}
	if dp, err := idx.Package(p.Author + "/" + p.Name); err == nil {
		r.Title = dp.Title
	}
	_, ver, err := a.Library.Ensure(ctx, r)
	if err != nil {
		return err
	}
	return a.switchLink(inst, l, ver)
}

// switchLink points a link at another version, recording the transition so repair can
// finish it after a crash on platforms without atomic replace.
func (a *App) switchLink(inst store.Installation, l store.Link, ver store.Version) error {
	if state := linkState(l, mustVersion(a, l.VersionID)); state == "replaced" {
		return fmt.Errorf("%s was replaced by a real directory (another tool updated it?); run `luanthetti repair --readopt`", l.Path)
	}
	l.PendingReplace = ver.Path
	if err := a.Store.UpdateLink(l); err != nil {
		return err
	}
	if err := linker.Replace(inst.Root, ver.Path, l.Path, l.Kind); err != nil {
		return err
	}
	l.VersionID, l.PendingReplace = ver.ID, ""
	return a.Store.UpdateLink(l)
}

func mustVersion(a *App, id int64) store.Version {
	v, _ := a.Store.VersionByID(id)
	return v
}

// installDeps links new dependencies of an updated package.
func (a *App) installDeps(ctx context.Context, idx *depgraph.Index, inst store.Installation, deps []string, requiredBy string) error {
	present := a.presentPackages(inst)
	var errs []string
	for _, key := range deps {
		p, err := idx.Package(key)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		item := a.planItem(ctx, idx, inst, p, "dependency", []string{requiredBy}, 0, present)
		if item.Status != "planned" {
			continue
		}
		if err := a.place(ctx, idx, inst, &item, nil); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", key, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Pin holds a package in an installation at its current release, or at release when > 0.
func (a *App) Pin(ctx context.Context, spec string, into []string, release int64, unpin bool) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.findLinkedPackage(spec)
	if err != nil {
		return nil, err
	}
	insts, err := a.selectInstalls(into)
	if err != nil {
		return nil, err
	}
	var done []string
	for _, inst := range insts {
		if _, ok := a.presentPackages(inst)[p.Key]; !ok {
			continue
		}
		if unpin {
			if err := a.Store.Unpin(inst.ID, p.ID); err != nil {
				return done, err
			}
		} else {
			var rel *int64
			if release > 0 {
				rel = &release
			}
			if err := a.Store.SetPin(store.Pin{InstallationID: inst.ID, PackageID: p.ID, ReleaseID: rel}); err != nil {
				return done, err
			}
		}
		done = append(done, inst.Name)
	}
	if len(done) == 0 {
		return nil, fmt.Errorf("%s/%s is not linked in the selected installations", p.Author, p.Name)
	}
	return done, nil
}

// Rollback switches a package back to the previous kept release (or to release when > 0).
func (a *App) Rollback(ctx context.Context, spec string, into []string, release int64) ([]UpdateItem, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.findLinkedPackage(spec)
	if err != nil {
		return nil, err
	}
	insts, err := a.selectInstalls(into)
	if err != nil {
		return nil, err
	}
	versions, _ := a.Store.VersionsOf(p.ID) // newest first
	var out []UpdateItem
	for _, inst := range insts {
		l, ok := a.presentPackages(inst)[p.Key]
		if !ok {
			continue
		}
		cur := mustVersion(a, l.VersionID)
		var to *store.Version
		for i := range versions {
			v := versions[i]
			if !v.Complete {
				continue
			}
			if release > 0 && v.ReleaseID == release || release == 0 && v.ReleaseID < cur.ReleaseID {
				to = &v
				break
			}
		}
		item := UpdateItem{Installation: inst.Name, Package: p.Author + "/" + p.Name, Folder: p.Folder, Current: cur.ReleaseID}
		if to == nil {
			item.Status, item.Note = "failed", "no earlier release kept in the library"
			out = append(out, item)
			continue
		}
		item.Latest = to.ReleaseID
		if err := a.switchLink(inst, l, *to); err != nil {
			item.Status, item.Note = "failed", err.Error()
		} else {
			item.Status = "updated"
			rel := to.ReleaseID
			_ = a.Store.SetPin(store.Pin{InstallationID: inst.ID, PackageID: p.ID, ReleaseID: &rel})
			item.Note = fmt.Sprintf("pinned to release %d so `update` doesn't undo it; `luanthetti unpin` releases it", rel)
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s/%s is not linked in the selected installations", p.Author, p.Name)
	}
	return out, nil
}
