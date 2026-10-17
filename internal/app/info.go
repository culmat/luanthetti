package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/culmat/luanthetti/internal/contentdb"
	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/store"
)

// Info describes a package: snapshot data, ContentDB details and releases (cached by release
// id), compatibility with the games of every installation (then the reference games), and
// where it is installed.
func (a *App) Info(ctx context.Context, spec string) (*InfoReport, error) {
	idx, err := a.Deps(ctx)
	if err != nil {
		return nil, err
	}
	p, err := idx.Package(spec)
	if err != nil {
		return nil, err
	}
	d := idx.Details(p)
	meta, _ := a.SnapshotMeta(ctx)
	rep := &InfoReport{Package: p.Key(), Type: p.Type, Title: p.Title, ShortDescription: d.ShortDescription,
		Provides: p.Provides.Sorted(), Depends: p.Depends.Sorted(), OptionalDepends: p.Optional.Sorted(),
		Score: p.Summary().Score, Downloads: p.Downloads, Details: d, Releases: []ReleaseInfo{}, Compat: []depgraph.CompatRow{},
		Installed: []ListRow{}, Screenshots: []string{}, Notes: []string{}, Source: "snapshot", SnapshotBuiltAt: meta.BuiltAt,
		URL: fmt.Sprintf("%s/packages/%s/%s/", a.CDB.Base, p.Author, p.Name)}

	detail, releases, source, err := a.packageDetail(ctx, p, d.ReleaseID)
	if err != nil {
		rep.Notes = append(rep.Notes, "ContentDB details unavailable: "+err.Error())
	}
	if detail != nil {
		rep.Source = source
		rep.Title = firstNonEmpty(detail.Title, rep.Title)
		rep.ShortDescription = firstNonEmpty(detail.ShortDescription, rep.ShortDescription)
		rep.Thumbnail = detail.Thumbnail
		rep.Screenshots = nonNil(detail.Screenshots)
		if detail.Downloads > 0 {
			rep.Downloads = detail.Downloads
		}
		if len(rep.Details.Tags) == 0 {
			rep.Details.Tags = detail.Tags
		}
		rep.Details.DevState = firstNonEmpty(rep.Details.DevState, detail.DevState)
		rep.Details.License = firstNonEmpty(rep.Details.License, detail.License)
		rep.Details.CreatedAt = firstNonEmpty(rep.Details.CreatedAt, detail.CreatedAt)
	}
	for _, r := range releases {
		ri := ReleaseInfo{ID: r.ID, Title: r.Title, Date: r.ReleaseDate}
		if r.MinEngine != nil {
			ri.MinEngine = r.MinEngine.Name
		}
		if r.MaxEngine != nil {
			ri.MaxEngine = r.MaxEngine.Name
		}
		rep.Releases = append(rep.Releases, ri)
	}

	// Games: those of the registered installations first, then the default games.
	var games []*depgraph.Pkg
	seen := map[int64]bool{}
	insts, _ := a.Store.Installations()
	for _, inst := range insts {
		known, _ := a.gamesOf(idx, inst)
		for _, g := range known {
			if !seen[g.pkg.ID] {
				seen[g.pkg.ID] = true
				games = append(games, g.pkg)
			}
		}
	}
	for _, g := range idx.DefaultGames(10) {
		if !seen[g.ID] {
			seen[g.ID] = true
			games = append(games, g)
		}
	}
	if p.Type != "game" {
		refs, _ := idx.PackageRefs(p)
		rows, err := idx.Compat(depgraph.TargetFromPkg(p), games, refs)
		if err == nil {
			rep.Compat = rows
		}
		rep.AnalyzedSources = len(refs) > 0
		if !rep.AnalyzedSources {
			rep.Notes = append(rep.Notes, "the package's code is not analysed in the snapshot, so `risky` (undeclared API use) cannot be detected")
		}
	}
	if sp, err := a.Store.PackageByKey(store.Key(p.Author, p.Name)); err == nil {
		for _, inst := range insts {
			rows, _ := a.listRows(inst, nil)
			for _, r := range rows {
				if strings.EqualFold(r.Package, sp.Author+"/"+sp.Name) {
					rep.Installed = append(rep.Installed, r)
				}
			}
		}
	}
	return rep, nil
}

// packageDetail returns ContentDB's detail and releases, cached by release id for 7 days.
func (a *App) packageDetail(ctx context.Context, p *depgraph.Pkg, release int64) (*contentdb.Detail, []contentdb.Release, string, error) {
	key := store.Key(p.Author, p.Name)
	cached, cerr := a.Store.CachedPackage(key)
	fresh := cerr == nil && (release == 0 || cached.ReleaseID >= release) && time.Since(cached.FetchedAt) < 7*24*time.Hour
	if cerr == nil && (fresh || a.Offline) {
		d, r := decodeCached(cached)
		return d, r, "cache", nil
	}
	if a.Offline {
		return nil, nil, "", fmt.Errorf("offline and not cached")
	}
	d, err := a.CDB.Package(ctx, p.Author, p.Name)
	if err != nil {
		if cerr == nil {
			dd, rr := decodeCached(cached)
			return dd, rr, "cache", nil
		}
		return nil, nil, "", err
	}
	releases, err := a.CDB.Releases(ctx, p.Author, p.Name)
	if err != nil {
		releases = nil
	}
	dj, _ := json.Marshal(d)
	rj, _ := json.Marshal(releases)
	_ = a.Store.PutCachedPackage(store.CachedPackage{Key: key, ReleaseID: d.Release, DetailJSON: dj, ReleasesJSON: rj})
	return d, releases, "live", nil
}

func decodeCached(c store.CachedPackage) (*contentdb.Detail, []contentdb.Release) {
	var d contentdb.Detail
	var r []contentdb.Release
	if json.Unmarshal(c.DetailJSON, &d) != nil {
		return nil, nil
	}
	_ = json.Unmarshal(c.ReleasesJSON, &r)
	return &d, r
}
