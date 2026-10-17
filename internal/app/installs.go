package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/culmat/luanthetti/internal/installs"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/store"
)

// AddInstallRequest is `installs add`.
type AddInstallRequest struct {
	Path          string
	Name          string
	Kind          string
	EngineVersion string
	LinkMode      string
	Create        bool // create mods/, games/, textures/ when the directory is empty or missing
}

// DetectInstalls lists installation candidates; with add, registers the new ones.
func (a *App) DetectInstalls(ctx context.Context, add bool) (*DetectReport, error) {
	cands := installs.Detect(ctx, false)
	installs.SortCandidates(cands)
	rep := &DetectReport{Candidates: []DetectedInstall{}, Added: []string{}}
	for _, c := range cands {
		d := DetectedInstall{Candidate: c}
		if inst, err := a.Store.Installation(c.Root); err == nil {
			d.Registered = inst.Name
		} else if add {
			inst, err := a.AddInstall(ctx, AddInstallRequest{Path: c.Root, Kind: c.Kind, EngineVersion: c.EngineVersion})
			if err != nil {
				return rep, err
			}
			d.Registered = inst.Name
			rep.Added = append(rep.Added, inst.Name)
		}
		rep.Candidates = append(rep.Candidates, d)
	}
	return rep, nil
}

// AddInstall registers an installation.
func (a *App) AddInstall(ctx context.Context, req AddInstallRequest) (store.Installation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	root, err := filepath.Abs(req.Path)
	if err != nil {
		return store.Installation{}, err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if !installs.LooksLikeInstallation(root) {
		if !req.Create {
			return store.Installation{}, fmt.Errorf("%s does not look like a Luanti installation (no mods/, games/, worlds/ or minetest.conf); pass --create to set one up", root)
		}
		for _, d := range []string{"mods", "games", "textures", "worlds"} {
			if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
				return store.Installation{}, err
			}
		}
	}
	if fsWithin(a.Library.Root, root) || fsWithin(root, a.Library.Root) {
		return store.Installation{}, errors.New("an installation cannot contain the library or be inside it")
	}
	if existing, err := a.Store.Installation(root); err == nil {
		return store.Installation{}, fmt.Errorf("%s is already registered as %q", root, existing.Name)
	}
	c := installs.Inspect(root)
	inst := store.Installation{Root: root, Kind: firstNonEmpty(req.Kind, c.Kind, "user"), EngineVersion: c.EngineVersion,
		EngineSource: c.EngineSource, BinaryPath: c.BinaryPath, LinkMode: req.LinkMode}
	if req.EngineVersion != "" {
		inst.EngineVersion, inst.EngineSource = req.EngineVersion, "manual"
	}
	if inst.EngineVersion == "" {
		// A plain user path has no binary; use the system Luanti if there is one.
		for _, b := range installs.FindBinaries(ctx) {
			if !b.RunInPlace {
				inst.EngineVersion, inst.EngineSource, inst.BinaryPath = b.Version, "binary", b.Path
				break
			}
		}
	}
	if inst.EngineVersion == "" {
		inst.EngineSource = "unknown"
		a.Log("%s: Luanti version unknown; updates are not filtered by engine version (set it with --engine-version)", root)
	}
	if inst.Kind == "user" {
		inst.ExtraGameDirs = installs.SystemGameDirs()
	}
	if inst.LinkMode == "" || inst.LinkMode == linker.Auto {
		inst.LinkMode = linker.Probe(filepath.Join(root, "mods"))
	}
	name, err := a.uniqueName(firstNonEmpty(req.Name, defaultName(inst)))
	if err != nil {
		return store.Installation{}, err
	}
	inst.Name = name
	return a.Store.AddInstallation(inst)
}

func defaultName(inst store.Installation) string {
	switch inst.Kind {
	case "user":
		return "desktop"
	case "flatpak":
		return "flatpak"
	}
	base := strings.ToLower(filepath.Base(inst.Root))
	base = strings.TrimPrefix(base, ".")
	if base == "" || base == "/" {
		return "luanti"
	}
	return base
}

func (a *App) uniqueName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid installation name %q", name)
	}
	candidate := name
	for i := 2; ; i++ {
		if _, err := a.Store.Installation(candidate); errors.Is(err, store.ErrNotFound) {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", name, i)
	}
}

// ListInstalls lists registered installations with their games.
func (a *App) ListInstalls(ctx context.Context) ([]InstallView, error) {
	all, err := a.Store.Installations()
	if err != nil {
		return nil, err
	}
	idx, idxErr := a.Deps(ctx)
	out := []InstallView{}
	for _, inst := range all {
		v := InstallView{Installation: inst, Games: []GameRef{}, UnknownGames: []GameRef{}, Exists: installs.LooksLikeInstallation(inst.Root)}
		if idxErr == nil {
			known, unknown := a.gamesOf(idx, inst)
			v.Games, v.UnknownGames = nonNil(known), nonNil(unknown)
		} else {
			for _, g := range installs.Games(inst.Root, inst.ExtraGameDirs...) {
				v.UnknownGames = append(v.UnknownGames, GameRef{ID: g.ID, Title: g.Title})
			}
		}
		links, _ := a.Store.LinksIn(inst.ID)
		v.Links = len(links)
		out = append(out, v)
	}
	return out, nil
}

// RemoveInstall deregisters an installation; with unlink it first removes every link.
func (a *App) RemoveInstall(ctx context.Context, name string, unlink bool) (*RemoveReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	inst, err := a.Store.Installation(name)
	if err != nil {
		return nil, fmt.Errorf("no installation %q", name)
	}
	rep := &RemoveReport{Removed: []RemovedLink{}, Library: []string{}, Warnings: []string{}}
	links, err := a.Store.LinksIn(inst.ID)
	if err != nil {
		return nil, err
	}
	if len(links) > 0 && !unlink {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d package(s) stay in %s as they are; pass --unlink to remove them first", len(links), inst.Root))
	}
	if unlink {
		for _, l := range links {
			pkg, _ := a.Store.PackageByID(l.PackageID)
			if err := linker.Unlink(inst.Root, l.Path, l.Kind); err != nil {
				return rep, err
			}
			rep.Removed = append(rep.Removed, RemovedLink{Installation: inst.Name, Package: pkg.Author + "/" + pkg.Name, Path: l.Path, Reason: l.Reason})
		}
	}
	return rep, a.Store.DeleteInstallation(inst.ID)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func fsWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
