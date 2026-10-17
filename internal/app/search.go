package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/culmat/luanthetti/internal/contentdb"
	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/store"
)

// Search finds packages in the snapshot (works offline) with filters on type, tags, maturity,
// popularity, maintenance and compatibility with the games of chosen installations. With
// Thumbnails it adds thumbnail URLs from one ContentDB list request (cached 10 minutes).
func (a *App) Search(ctx context.Context, q SearchQuery) (*SearchReport, error) {
	idx, err := a.Deps(ctx)
	if err != nil {
		return nil, err
	}
	rep := &SearchReport{Hits: []SearchHit{}, Games: []GameRef{}, Notes: []string{}}
	games, err := a.searchGames(idx, q.CompatibleWith, rep)
	if err != nil {
		return nil, err
	}
	if !idx.HasTags() && (len(q.Tags) > 0 || q.UpdatedWithin > 0) {
		rep.Notes = append(rep.Notes, "this dependency snapshot has no tags or release dates (luanti-depgraph older than 0.3.2); those filters match nothing")
		rep.Degraded = true
	}
	installedIn := a.installedIndex()
	needle := strings.ToLower(strings.TrimSpace(q.Q))
	cutoff := time.Time{}
	if q.UpdatedWithin > 0 {
		cutoff = time.Now().Add(-q.UpdatedWithin)
	}
	type scored struct {
		hit  SearchHit
		tier int // 0 package named like the query, 1 a mod providing it, 2 a game providing it, 3 text match
		date string
	}
	var all []scored
	for _, p := range idx.All() {
		if q.Type != "" && p.Type != q.Type {
			continue
		}
		if q.Type == "" && p.Type == "game" && len(q.CompatibleWith) > 0 {
			continue // games are not "compatible with" games
		}
		d := idx.Details(p)
		tier := 3
		if needle != "" {
			hay := strings.ToLower(p.Key() + " " + p.Title + " " + d.ShortDescription)
			switch {
			case needle == strings.ToLower(p.Name):
				tier = 0
			case p.Provides.Has(needle) && p.Type != "game":
				tier = 1
			case p.Provides.Has(needle):
				tier = 2
			case !strings.Contains(hay, needle):
				continue
			}
		}
		if !hasAllTags(d.Tags, q.Tags) || !matchesAny(d.DevState, q.DevStates) {
			continue
		}
		if q.MinDownloads > 0 && p.Downloads < q.MinDownloads {
			continue
		}
		if q.MinScore > 0 && p.Score < q.MinScore {
			continue
		}
		if !cutoff.IsZero() {
			t, err := parseCDBTime(d.LatestReleaseAt)
			if err != nil || t.Before(cutoff) {
				continue
			}
		}
		hit := SearchHit{Package: p.Key(), Type: p.Type, Title: p.Title, ShortDescription: d.ShortDescription,
			Score: p.Summary().Score, Downloads: p.Downloads, DevState: d.DevState, Tags: d.Tags, LatestReleaseAt: d.LatestReleaseAt,
			Release: d.ReleaseID, Compat: map[string]string{}, InstalledIn: nonNil(installedIn[store.Key(p.Author, p.Name)])}
		if p.Type != "game" && len(games) > 0 {
			works := false
			for _, g := range games {
				s := depgraph.StatusWord(idx.StatusIn(p, g.pkg))
				hit.Compat[g.Key] = s
				works = works || s == "ok" || s == "risky"
			}
			if len(q.CompatibleWith) > 0 && !works {
				continue
			}
		}
		all = append(all, scored{hit: hit, tier: tier, date: d.LatestReleaseAt})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].tier != all[j].tier {
			return all[i].tier < all[j].tier
		}
		switch q.Sort {
		case "downloads":
			return all[i].hit.Downloads > all[j].hit.Downloads
		case "updated":
			return all[i].date > all[j].date
		case "name":
			return all[i].hit.Package < all[j].hit.Package
		}
		return all[i].hit.Score > all[j].hit.Score
	})
	rep.Total = len(all)
	limit := q.Limit
	if limit <= 0 {
		limit = 30
	}
	for i := 0; i < len(all) && i < limit; i++ {
		rep.Hits = append(rep.Hits, all[i].hit)
	}
	if q.Thumbnails && len(rep.Hits) > 0 {
		a.addThumbnails(ctx, q, rep)
	}
	return rep, nil
}

// searchGames resolves --compatible-with (installation names or game names); without it, the
// games of every installation are used for the compat marks only.
func (a *App) searchGames(idx *depgraph.Index, names []string, rep *SearchReport) ([]GameRef, error) {
	var games []GameRef
	seen := map[string]bool{}
	add := func(g GameRef) {
		if !seen[g.Key] {
			seen[g.Key] = true
			games = append(games, g)
		}
	}
	if len(names) == 0 {
		insts, _ := a.Store.Installations()
		for _, inst := range insts {
			known, _ := a.gamesOf(idx, inst)
			for _, g := range known {
				add(g)
			}
		}
	}
	for _, n := range names {
		if inst, err := a.Store.Installation(n); err == nil {
			known, _ := a.gamesOf(idx, inst)
			for _, g := range known {
				add(g)
			}
			continue
		}
		g, err := idx.Game(n)
		if err != nil {
			return nil, err
		}
		add(GameRef{ID: g.Name, Key: g.Key(), Title: g.Title, pkg: g})
	}
	rep.Games = nonNil(games)
	return games, nil
}

// installedIndex maps lower(author)/name to the installations it is linked in.
func (a *App) installedIndex() map[string][]string {
	out := map[string][]string{}
	insts, _ := a.Store.Installations()
	for _, inst := range insts {
		for key := range a.presentPackages(inst) {
			out[key] = append(out[key], inst.Name)
		}
	}
	return out
}

func (a *App) addThumbnails(ctx context.Context, q SearchQuery, rep *SearchReport) {
	cacheKey := "search:" + strings.ToLower(q.Q) + ":" + q.Type
	var list []contentdb.ShortPackage
	if data, fetched, err := a.Store.CachedBlob(cacheKey); err == nil && (a.Offline || time.Since(fetched) < 10*time.Minute) {
		_ = json.Unmarshal(data, &list)
	} else if !a.Offline {
		lq := contentdb.ListQuery{Q: q.Q, Limit: 200}
		if q.Type != "" {
			lq.Types = []string{q.Type}
		}
		if got, err := a.CDB.ListPackages(ctx, lq); err == nil {
			list = got
			data, _ := json.Marshal(got)
			_ = a.Store.PutCachedBlob(cacheKey, data)
		} else {
			rep.Notes = append(rep.Notes, "thumbnails unavailable: "+err.Error())
		}
	}
	thumbs := map[string]string{}
	for _, sp := range list {
		thumbs[sp.Key()] = sp.Thumbnail
	}
	for i := range rep.Hits {
		author, name, _ := strings.Cut(rep.Hits[i].Package, "/")
		rep.Hits[i].Thumbnail = thumbs[store.Key(author, name)]
	}
}

func hasAllTags(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if strings.EqualFold(h, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func matchesAny(v string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.ReplaceAll(a, "-", "_"), v) {
			return true
		}
	}
	return false
}

func parseCDBTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, &time.ParseError{Value: s}
}
