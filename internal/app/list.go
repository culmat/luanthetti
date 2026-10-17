package app

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/luanticonf"
	"github.com/culmat/luanthetti/internal/store"
)

// ListRequest is `list`.
type ListRequest struct {
	Into     []string
	Type     string // mod | game | txp
	Outdated bool
	Library  bool
}

// List lists linked packages per installation, and optionally the library.
func (a *App) List(ctx context.Context, req ListRequest) (*ListReport, error) {
	rep := &ListReport{Rows: []ListRow{}}
	insts, err := a.Store.Installations()
	if err != nil {
		return nil, err
	}
	if len(req.Into) > 0 {
		if insts, err = a.selectInstalls(req.Into); err != nil {
			return nil, err
		}
	}
	for _, inst := range insts {
		var latest map[string]int64
		if req.Outdated {
			latest, _ = a.latestReleases(ctx, inst.EngineVersion, inst.Protocol, false)
		}
		rows, err := a.listRows(inst, latest)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if req.Type != "" && r.Type != req.Type {
				continue
			}
			if req.Outdated && !r.Outdated {
				continue
			}
			rep.Rows = append(rep.Rows, r)
		}
	}
	if req.Library {
		pkgs, err := a.Store.Packages()
		if err != nil {
			return nil, err
		}
		for _, p := range pkgs {
			if req.Type != "" && p.Type != req.Type {
				continue
			}
			row := LibraryRow{Package: p.Author + "/" + p.Name, Type: p.Type, Source: p.Source, Versions: []LibraryVersion{}}
			versions, _ := a.Store.VersionsOf(p.ID)
			for _, v := range versions {
				n, _ := a.Store.LinkCount(v.ID)
				row.Versions = append(row.Versions, LibraryVersion{Release: v.ReleaseID, Title: v.Title, Links: n, Origin: v.Origin, Path: v.Path})
			}
			rep.Library = append(rep.Library, row)
		}
	}
	return rep, nil
}

// listRows describes every link of an installation and its state on disk.
func (a *App) listRows(inst store.Installation, latest map[string]int64) ([]ListRow, error) {
	links, err := a.Store.LinksIn(inst.ID)
	if err != nil {
		return nil, err
	}
	var out []ListRow
	for _, l := range links {
		p, err := a.Store.PackageByID(l.PackageID)
		if err != nil {
			continue
		}
		v, _ := a.Store.VersionByID(l.VersionID)
		pin, _ := a.Store.Pin(inst.ID, p.ID)
		row := ListRow{Installation: inst.Name, Package: p.Author + "/" + p.Name, Type: p.Type, FolderType: p.FolderType,
			Folder: filepath.Base(l.Path), Release: v.ReleaseID, ReleaseTitle: v.Title, Kind: l.Kind, Reason: l.Reason,
			RequiredBy: l.RequiredBy, Pinned: pin != nil, Path: l.Path, State: linkState(l, v)}
		if latest != nil && p.Source == "contentdb" {
			if rel, ok := latest[p.Key]; ok {
				row.Latest = rel
				row.Outdated = rel > v.ReleaseID
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// linkState checks a link on disk: ok, missing, replaced (a real directory where a link was
// recorded), broken (dangling link), wrong_target, or conf_mismatch (a copy whose release
// changed under us).
func linkState(l store.Link, v store.Version) string {
	if l.Kind == linker.Env {
		return "ok"
	}
	in, err := linker.Inspect(l.Path)
	if err != nil || !in.Exists {
		return "missing"
	}
	switch l.Kind {
	case linker.Symlink, linker.Junction:
		if !in.IsLink {
			return "replaced"
		}
		if in.Dangling {
			return "broken"
		}
		if filepath.Clean(in.Target) != filepath.Clean(v.Path) {
			return "wrong_target"
		}
	case linker.Copy:
		if in.IsLink {
			return "wrong_target"
		}
	}
	if v.ReleaseID > 0 {
		for _, conf := range []string{"mod.conf", "modpack.conf", "game.conf", "texture_pack.conf"} {
			c := luanticonf.ReadConf(filepath.Join(l.Path, conf))
			if r, ok := c["release"]; ok {
				if r != strconv.FormatInt(v.ReleaseID, 10) {
					return "conf_mismatch"
				}
				break
			}
		}
	}
	return "ok"
}
