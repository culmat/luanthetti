package depgraph

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/culmat/luanthetti/internal/luanticonf"
)

var modnameRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// IsLocal reports whether spec names a local directory rather than a ContentDB package.
func IsLocal(spec string) bool {
	if spec == "." || spec == ".." || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "./") ||
		strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "~") {
		return true
	}
	if filepath.IsAbs(spec) || strings.HasPrefix(spec, `.\`) || strings.HasPrefix(spec, `..\`) {
		return true
	}
	st, err := os.Stat(spec)
	return err == nil && st.IsDir()
}

func expandHome(spec string) string {
	if strings.HasPrefix(spec, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, spec[1:])
		}
	}
	return spec
}

// TargetFor turns a spec into a target: a scanned local directory (pkg nil) or a package.
func (idx *Index) TargetFor(spec string) (Target, *Pkg, error) {
	if IsLocal(spec) {
		scanned, err := luanticonf.ScanPackage(expandHome(spec))
		if err != nil {
			return Target{}, nil, err
		}
		return TargetFromScan(scanned), nil, nil
	}
	p, err := idx.Package(spec)
	if err != nil {
		return Target{}, nil, err
	}
	return TargetFromPkg(p), p, nil
}

// ProvidersReport is ldg's `providers`: who provides a mod name, mods first, then games.
func (idx *Index) ProvidersReport(modname string) ProvidersReport {
	found := idx.Providers(modname)
	rep := ProvidersReport{Modname: modname, Count: len(found), Mods: []Summary{}, Games: []Summary{}}
	for _, p := range found {
		if p.Type == "game" {
			rep.Games = append(rep.Games, p.Summary())
		} else {
			rep.Mods = append(rep.Mods, p.Summary())
		}
	}
	if len(found) == 0 {
		note := "No ContentDB package provides this mod name."
		rep.Note = &note
	}
	return rep
}

// Resolve is ldg's `resolve`: the target in the named game.
func (idx *Index) Resolve(spec, game string) (*Resolution, error) {
	t, pkg, err := idx.TargetFor(spec)
	if err != nil {
		return nil, err
	}
	g, err := idx.Game(game)
	if err != nil {
		return nil, err
	}
	if pkg != nil {
		if res, ok := idx.PrecomputedResolution(pkg, g); ok {
			return res, nil
		}
	}
	return NewResolver(idx, g).Resolve(t), nil
}

// CompatReport is ldg's `compat_report`: one row per game (the default games when none given).
func (idx *Index) CompatReport(spec string, games []string) (*CompatReport, error) {
	t, pkg, err := idx.TargetFor(spec)
	if err != nil {
		return nil, err
	}
	var refs []Ref
	if pkg != nil {
		if refs, err = idx.PackageRefs(pkg); err != nil {
			return nil, err
		}
	}
	var gamePkgs []*Pkg
	if len(games) == 0 {
		gamePkgs = idx.DefaultGames(10)
	} else {
		for _, g := range games {
			p, err := idx.Game(g)
			if err != nil {
				return nil, err
			}
			gamePkgs = append(gamePkgs, p)
		}
	}
	rows, err := idx.Compat(t, gamePkgs, refs)
	if err != nil {
		return nil, err
	}
	return &CompatReport{Target: t.Key, AnalyzedSources: len(refs) > 0, Games: rows}, nil
}

// Search is ldg's `search`: case-insensitive substring match on "author/name title", or an
// exact provided mod name; exact matches first, then by score.
func (idx *Index) Search(query, typ string, limit int) []SearchHit {
	q := strings.ToLower(query)
	type hit struct {
		notExact bool
		p        *Pkg
	}
	var hits []hit
	for _, p := range idx.order {
		if typ != "" && p.Type != typ {
			continue
		}
		hay := strings.ToLower(p.Key() + " " + p.Title)
		if strings.Contains(hay, q) || p.Provides.Has(q) {
			exact := q == p.Name || p.Provides.Has(q)
			hits = append(hits, hit{!exact, p})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].notExact != hits[j].notExact {
			return !hits[i].notExact
		}
		return hits[i].p.Score > hits[j].p.Score
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]SearchHit, 0, len(hits))
	for _, h := range hits {
		provides := h.p.Provides.Sorted()
		if len(provides) > 20 {
			provides = provides[:20]
		}
		out = append(out, SearchHit{Summary: h.p.Summary(), Provides: provides})
	}
	return out
}

// NameCheck is ldg's `name_check`: is a mod name free on ContentDB?
func (idx *Index) NameCheck(modname, author string) NameCheck {
	valid := modnameRE.MatchString(modname)
	var users, named []*Pkg
	for _, p := range idx.ProvidersOf[modname] {
		if author == "" || p.Author != author {
			users = append(users, p)
		}
	}
	for _, p := range idx.byName[modname] {
		if author == "" || p.Author != author {
			named = append(named, p)
		}
	}
	nc := NameCheck{Modname: modname, Valid: valid, UsedBy: []string{}, BundledInGames: []string{},
		Policy: "https://content.luanti.org/policy_and_guidance/ (3.1 right to a name, 3.2 forks/drop-in replacements, 3.3 game prefixes)"}
	for _, p := range users {
		if p.Type != "game" {
			nc.UsedBy = append(nc.UsedBy, p.Key())
		} else {
			nc.BundledInGames = append(nc.BundledInGames, p.Key())
		}
	}
	for _, p := range named {
		if !containsPkg(users, p) {
			nc.UsedBy = append(nc.UsedBy, p.Key())
		}
	}
	nc.Available = valid && len(nc.UsedBy) == 0
	return nc
}
