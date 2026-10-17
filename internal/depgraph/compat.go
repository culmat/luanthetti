package depgraph

import (
	"database/sql"
	"sort"
	"strings"
)

// PackageRefs returns the static-analysis references recorded for a package's mods
// (empty when the package is not in the corpus or the snapshot has no analysis).
func (idx *Index) PackageRefs(p *Pkg) ([]Ref, error) {
	if !idx.HasRefs {
		return nil, nil
	}
	rows, err := idx.conn.Query(`SELECT m.name, r.modname, r.kind, r.symbol, r.count, r.file, r.line,
		r.declared, r.transitive, r.guarded, r.requires FROM ref r JOIN mod m ON m.id = r.mod_id
		WHERE m.package_id = ? ORDER BY r.rowid`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []Ref
	for rows.Next() {
		var r Ref
		var file sql.NullString
		var line sql.NullInt64
		var declared, transitive, guarded int
		var requires string
		if err := rows.Scan(&r.Mod, &r.Modname, &r.Kind, &r.Symbol, &r.Count, &file, &line, &declared, &transitive, &guarded, &requires); err != nil {
			return nil, err
		}
		r.File, r.Line = file.String, int(line.Int64)
		r.Declared, r.Transitive, r.Guarded = declared != 0, transitive != 0, guarded != 0
		for _, x := range strings.Split(requires, ",") {
			if x != "" {
				r.Requires = append(r.Requires, x)
			}
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// IsRisky: an undeclared, unguarded use of another mod's API (a global) that the game doesn't
// have. Undeclared item names only leave recipes or lookups unused, so they don't count.
func IsRisky(r Ref, available Set) bool {
	for _, req := range r.Requires {
		if !available.Has(req) {
			return false // the file using it isn't even loaded in this game
		}
	}
	return r.Kind == "global" && !(r.Declared || r.Transitive || r.Guarded) && !available.Has(r.Modname)
}

// Claims returns ContentDB's own game-support rows for a package (all confidences).
func (idx *Index) Claims(p *Pkg) (map[int64]Claim, error) {
	rows, err := idx.conn.Query("SELECT game_id, supports, confidence FROM game_support WHERE package_id = ?", p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Claim{}
	for rows.Next() {
		var gid int64
		var supports, confidence int
		if err := rows.Scan(&gid, &supports, &confidence); err != nil {
			return nil, err
		}
		out[gid] = Claim{Supports: supports != 0, Confidence: confidence}
	}
	return out, rows.Err()
}

// availableAfter is the set of mod names present once the resolution's installs are in place.
func (idx *Index) availableAfter(game *Pkg, t Target, res *Resolution) Set {
	available := game.Provides.Union(t.Provides)
	for _, e := range res.Install {
		if p, ok := idx.byKey[e.Package]; ok {
			for m := range p.Provides {
				available.Add(m)
			}
		}
	}
	return available
}

func riskyRefs(refs []Ref, available Set) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		if IsRisky(r, available) {
			k := r.Modname + " (" + r.Kind + ")"
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out) // "(modname, kind)" tuples sort the same as these strings
	return out
}

// Compat gives one row per game: can the target be installed, and do its undeclared
// references hold? refs are the target's static-analysis references (nil for local targets).
func (idx *Index) Compat(t Target, games []*Pkg, refs []Ref) ([]CompatRow, error) {
	var claims map[int64]Claim
	if !t.IsLocal() {
		p, err := idx.Package(t.Key)
		if err != nil {
			return nil, err
		}
		if claims, err = idx.Claims(p); err != nil {
			return nil, err
		}
	}
	var pkg *Pkg
	if !t.IsLocal() {
		pkg, _ = idx.Package(t.Key)
	}
	rows := make([]CompatRow, 0, len(games))
	for _, game := range games {
		if pkg != nil {
			if row, ok := idx.PrecomputedCompat(pkg, game); ok {
				if c, ok := claims[game.ID]; ok {
					cc := c
					row.ContentDBSays = &cc
				}
				rows = append(rows, row)
				continue
			}
		}
		res := NewResolver(idx, game).Resolve(t)
		risky := riskyRefs(refs, idx.availableAfter(game, t, res))
		status := "ok"
		switch {
		case !res.OK:
			status = "unsatisfiable"
		case len(risky) > 0:
			status = "risky"
		}
		row := CompatRow{Game: game.Key(), Title: game.Title, Status: status,
			Missing: []string{}, Install: []string{}, UndeclaredMissing: strs(risky), OptionalAvailable: res.OptionalAvailable}
		for _, m := range res.Missing {
			row.Missing = append(row.Missing, m.Modname)
		}
		for _, e := range res.Install {
			row.Install = append(row.Install, e.Package)
		}
		if c, ok := claims[game.ID]; ok {
			cc := c
			row.ContentDBSays = &cc
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Status is 'o' ok, 'r' risky, 'u' unsatisfiable for one package in the resolver's game,
// the compact form used for compat marks on search results.
func (idx *Index) Status(r *Resolver, p *Pkg) (byte, error) {
	t := TargetFromPkg(p)
	res := r.Resolve(t)
	if !res.OK {
		return 'u', nil
	}
	refs, err := idx.PackageRefs(p)
	if err != nil {
		return 0, err
	}
	available := idx.availableAfter(r.Game, t, res)
	for _, ref := range refs {
		if IsRisky(ref, available) {
			return 'r', nil
		}
	}
	return 'o', nil
}
