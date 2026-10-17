package depgraph

import "strings"

// GameForInstalled maps a game folder found in an installation to its snapshot package: by
// the in-game client's ContentDB identity (author/id, also tried as author/id_game, as the
// update detector does), then by game id and title.
func (idx *Index) GameForInstalled(id, author, title string) *Pkg {
	if author != "" {
		for _, key := range []string{author + "/" + id, author + "/" + id + "_game"} {
			if p, err := idx.Package(key); err == nil && p.Type == "game" {
				return p
			}
		}
	}
	for _, spec := range []string{id, id + "_game", title} {
		if strings.TrimSpace(spec) == "" {
			continue
		}
		if g, err := idx.Game(spec); err == nil {
			return g
		}
	}
	return nil
}

// StatusIn is the compact compat status of a package in a game ('o', 'r', 'u'), using the
// precomputed matrix when available.
func (idx *Index) StatusIn(p, game *Pkg) byte {
	if row, ok := idx.PrecomputedCompat(p, game); ok {
		switch row.Status {
		case "ok":
			return 'o'
		case "risky":
			return 'r'
		default:
			return 'u'
		}
	}
	s, err := idx.Status(idx.ResolverFor(game), p)
	if err != nil {
		return '?'
	}
	return s
}

// ResolveIn is the resolution of a package in a game, precomputed when available.
func (idx *Index) ResolveIn(p, game *Pkg) *Resolution {
	if res, ok := idx.PrecomputedResolution(p, game); ok {
		return res
	}
	return idx.ResolverFor(game).Resolve(TargetFromPkg(p))
}

// CompatIn is the full compat row of a package in a game, with ContentDB's claim.
func (idx *Index) CompatIn(p, game *Pkg) CompatRow {
	refs, _ := idx.PackageRefs(p)
	rows, err := idx.Compat(TargetFromPkg(p), []*Pkg{game}, refs)
	if err != nil || len(rows) == 0 {
		return CompatRow{Game: game.Key(), Title: game.Title, Status: "unknown", Missing: []string{}, Install: []string{},
			UndeclaredMissing: []string{}, OptionalAvailable: []string{}}
	}
	return rows[0]
}

// StatusWord turns a compact status into ok, risky, unsatisfiable or unknown.
func StatusWord(b byte) string {
	switch b {
	case 'o':
		return "ok"
	case 'r':
		return "risky"
	case 'u':
		return "unsatisfiable"
	}
	return "unknown"
}
