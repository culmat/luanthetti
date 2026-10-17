package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/store"
)

// RepairRequest is `repair`.
type RepairRequest struct {
	Into    []string
	Relink  bool // recreate missing/broken/wrong links, finish interrupted replacements
	Readopt bool // turn replaced folders back into managed versions
	DryRun  bool
}

// Verify checks every link (exists, right kind and target, version complete, conf release),
// scans for foreign or broken links and replaced folders, and resolves explicit packages
// against each game for dependencies that went missing.
func (a *App) Verify(ctx context.Context, into []string) (*VerifyReport, error) {
	insts, err := a.selectInstalls(into)
	if err != nil {
		return nil, err
	}
	rep := &VerifyReport{Problems: []Problem{}, Repaired: []Problem{}}
	idx, idxErr := a.Deps(ctx)
	for _, inst := range insts {
		links, _ := a.Store.LinksIn(inst.ID)
		present := a.presentPackages(inst)
		for _, l := range links {
			p, _ := a.Store.PackageByID(l.PackageID)
			v, verr := a.Store.VersionByID(l.VersionID)
			pkg := p.Author + "/" + p.Name
			if l.PendingReplace != "" {
				rep.Problems = append(rep.Problems, Problem{Installation: inst.Name, Path: l.Path, Package: pkg, Kind: "pending_replace",
					Detail: "an update was interrupted", Fix: "luanthetti repair --relink"})
				continue
			}
			if verr != nil || !v.Complete {
				rep.Problems = append(rep.Problems, Problem{Installation: inst.Name, Path: l.Path, Package: pkg, Kind: "incomplete",
					Detail: "the library version is missing or incomplete", Fix: "luanthetti update or reinstall"})
				continue
			}
			if _, err := os.Stat(v.Path); err != nil {
				rep.Problems = append(rep.Problems, Problem{Installation: inst.Name, Path: l.Path, Package: pkg, Kind: "incomplete",
					Detail: "library folder missing: " + v.Path, Fix: "reinstall the package"})
				continue
			}
			state := linkState(l, v)
			if state == "ok" {
				rep.OK++
				continue
			}
			pr := Problem{Installation: inst.Name, Path: l.Path, Package: pkg, Kind: state}
			switch state {
			case "missing":
				pr.Detail, pr.Fix = "the link was removed", "luanthetti repair --relink (or `luanthetti remove` to forget it)"
			case "broken":
				pr.Kind, pr.Detail, pr.Fix = "broken_link", "the link points nowhere", "luanthetti repair --relink"
			case "wrong_target":
				pr.Detail, pr.Fix = "the link points somewhere else", "luanthetti repair --relink"
			case "replaced":
				pr.Detail, pr.Fix = "a real folder replaced the link (another tool updated it)", "luanthetti repair --readopt"
			case "conf_mismatch":
				pr.Detail, pr.Fix = "the release in the conf differs from the library version", "luanthetti repair --readopt"
			}
			rep.Problems = append(rep.Problems, pr)
		}
		results, _ := a.scan(inst)
		for _, r := range results {
			if r.Status == "foreign_link" || (r.Status == "broken_link" && !isRecorded(links, r.Path)) {
				rep.Problems = append(rep.Problems, Problem{Installation: inst.Name, Path: r.Path, Kind: r.Status, Detail: r.Detail})
			}
		}
		if idxErr != nil {
			continue
		}
		games, _ := a.gamesOf(idx, inst)
		for _, l := range links {
			if l.Reason != "explicit" {
				continue
			}
			p, _ := a.Store.PackageByID(l.PackageID)
			dp, err := idx.Package(p.Author + "/" + p.Name)
			if err != nil || dp.Type == "game" {
				continue
			}
			for _, g := range games {
				res := idx.ResolveIn(dp, g.pkg)
				if !res.OK {
					continue
				}
				for _, e := range res.Install {
					ep, err := idx.Package(e.Package)
					if err != nil {
						continue
					}
					if _, ok := present[store.Key(ep.Author, ep.Name)]; ok || providedByPresent(idx, ep, present, a) {
						continue
					}
					rep.Problems = append(rep.Problems, Problem{Installation: inst.Name, Path: l.Path, Package: dp.Key(), Kind: "missing_dependency",
						Detail: fmt.Sprintf("needs %s in %s", ep.Key(), g.Key), Fix: "luanthetti install " + dp.Key() + " --into " + inst.Name})
				}
			}
		}
	}
	return rep, nil
}

func isRecorded(links []store.Link, path string) bool {
	for _, l := range links {
		if filepath.Clean(l.Path) == filepath.Clean(path) {
			return true
		}
	}
	return false
}

// providedByPresent: a dependency counts as present when an installed package provides the
// same mod names (a different provider than the resolver's first choice).
func providedByPresent(idx *depgraph.Index, dep *depgraph.Pkg, present map[string]store.Link, a *App) bool {
	for key := range present {
		p, err := a.Store.PackageByKey(key)
		if err != nil {
			continue
		}
		pp, err := idx.Package(p.Author + "/" + p.Name)
		if err != nil {
			continue
		}
		all := true
		for m := range dep.Provides {
			if !pp.Provides.Has(m) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// Repair fixes what Verify reports where it safely can.
func (a *App) Repair(ctx context.Context, req RepairRequest) (*VerifyReport, error) {
	rep, err := a.Verify(ctx, req.Into)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var left []Problem
	for _, pr := range rep.Problems {
		inst, err := a.Store.Installation(pr.Installation)
		if err != nil {
			left = append(left, pr)
			continue
		}
		fixed := false
		switch pr.Kind {
		case "missing", "broken_link", "wrong_target", "pending_replace":
			if req.Relink {
				fixed = req.DryRun || a.relink(inst, pr.Path) == nil
			}
		case "replaced", "conf_mismatch":
			if req.Readopt {
				if req.DryRun {
					fixed = true
				} else {
					ar, err := a.adoptLocked(ctx, inst, pr.Path)
					fixed = err == nil && len(ar.Adopted) == 1
				}
			}
		}
		if fixed {
			rep.Repaired = append(rep.Repaired, pr)
		} else {
			left = append(left, pr)
		}
	}
	rep.Problems = nonNil(left)
	return rep, nil
}

func (a *App) relink(inst store.Installation, path string) error {
	l, err := a.Store.LinkAt(inst.ID, path)
	if err != nil {
		return err
	}
	v, err := a.Store.VersionByID(l.VersionID)
	if err != nil {
		return err
	}
	if l.PendingReplace != "" {
		// Finish the interrupted replacement towards the version it was going to.
		pkgVersions, _ := a.Store.VersionsOf(l.PackageID)
		for _, pv := range pkgVersions {
			if pv.Path == l.PendingReplace {
				v = pv
			}
		}
	}
	in, _ := linker.Inspect(path)
	if in.Exists && !in.IsLink && l.Kind != linker.Copy {
		return fmt.Errorf("%s is a real folder; use --readopt", path)
	}
	if in.Exists {
		if err := linker.Unlink(inst.Root, path, l.Kind); err != nil {
			return err
		}
	}
	kind, err := linker.Link(v.Path, path, l.Kind)
	if err != nil {
		return err
	}
	l.Kind, l.VersionID, l.PendingReplace = kind, v.ID, ""
	return a.Store.UpdateLink(l)
}

// adoptLocked adopts one path (the caller holds a.mu).
func (a *App) adoptLocked(ctx context.Context, inst store.Installation, path string) (*AdoptReport, error) {
	a.mu.Unlock()
	defer a.mu.Lock()
	return a.Adopt(ctx, AdoptRequest{Installation: inst.Name, Specs: []string{filepath.Base(path)}, Force: true})
}
