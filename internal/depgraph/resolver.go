package depgraph

import (
	"fmt"
	"strings"
)

// Resolver answers whether packages can be installed in one game. It memoises per game, so
// reuse one Resolver for many targets in the same game.
type Resolver struct {
	idx           *Index
	Game          *Pkg
	gameNames     Set // the game's mod names, minus MTG copies in the known non-standard games
	applyDeclared bool
	memo          map[string]bool
}

// NewResolver creates a resolver for a game.
func NewResolver(idx *Index, game *Pkg) *Resolver { return newResolver(idx, game, true) }

func newResolver(idx *Index, game *Pkg, applyDeclared bool) *Resolver {
	names := game.Provides.Copy()
	if GamesWithNonstandardMTGCopies.Has(game.Name) {
		names = names.Minus(MTGMods)
	}
	return &Resolver{idx: idx, Game: game, gameNames: names, applyDeclared: applyDeclared, memo: map[string]bool{}}
}

// declaredOK: a provider that declares its games only counts in those (as on ContentDB).
func (r *Resolver) declaredOK(p *Pkg) bool {
	if !r.applyDeclared {
		return true
	}
	if r.idx.declaredUnsupported[p.ID][r.Game.ID] {
		return false
	}
	declared := r.idx.declaredSupport[p.ID]
	if len(declared) > 0 && !r.idx.supportsAll[p.ID] && !declared[r.Game.ID] {
		return !r.idx.GameIndependent(p)
	}
	return true
}

func inStack(stack []string, key string) bool {
	for _, k := range stack {
		if k == key {
			return true
		}
	}
	return false
}

// satisfiable returns (ok, cycleCut). A result influenced by a cut cycle isn't memoised.
func (r *Resolver) satisfiable(p *Pkg, stack []string) (bool, bool) {
	if p.Type == "game" {
		return false, false
	}
	key := p.Key()
	if v, ok := r.memo[key]; ok {
		return v, false
	}
	if inStack(stack, key) {
		return false, true
	}
	if !r.declaredOK(p) {
		r.memo[key] = false
		return false, false
	}
	stack = append(stack[:len(stack):len(stack)], key)
	cut := false
	for _, name := range p.Depends.Minus(p.Provides).Sorted() {
		ok, c := r.nameOK(name, stack)
		cut = cut || c
		if !ok {
			if !cut {
				r.memo[key] = false
			}
			return false, cut
		}
	}
	r.memo[key] = true
	return true, cut
}

func (r *Resolver) nameOK(name string, stack []string) (bool, bool) {
	if r.gameNames.Has(name) {
		return true, false
	}
	cut := false
	for _, p := range r.idx.Providers(name) {
		ok, c := r.satisfiable(p, stack)
		cut = cut || c
		if ok {
			return true, cut
		}
	}
	return false, cut
}

// BestProvider is the first provider in installer order whose own dependencies can be met.
func (r *Resolver) BestProvider(name string) *Pkg {
	for _, p := range r.idx.Providers(name) {
		if ok, _ := r.satisfiable(p, nil); ok {
			return p
		}
	}
	return nil
}

func (r *Resolver) explainMissing(name, neededBy string) Missing {
	found := r.idx.Providers(name)
	m := Missing{Modname: name, NeededBy: neededBy, Providers: len(found), Candidates: []Candidate{}, OnlyInGames: []string{}}
	var games []string
	for _, p := range found {
		if p.Type == "game" {
			games = append(games, p.Key())
		}
	}
	for _, p := range found {
		if p.Type == "game" {
			continue
		}
		var failing []string
		for _, d := range p.Depends.Minus(p.Provides).Sorted() {
			if ok, _ := r.nameOK(d, []string{p.Key()}); !ok {
				failing = append(failing, d)
			}
		}
		problem := "dependency cycle"
		if len(failing) > 0 {
			problem = fmt.Sprintf("needs %s, not available in this game", strings.Join(failing, ", "))
		}
		m.Candidates = append(m.Candidates, Candidate{Package: p.Key(), Problem: problem})
		if len(m.Candidates) == 10 {
			break
		}
	}
	if len(games) > 10 {
		games = games[:10]
	}
	m.OnlyInGames = strs(games)
	m.GameProviders = countGames(found)
	return m
}

func countGames(list []*Pkg) int {
	n := 0
	for _, p := range list {
		if p.Type == "game" {
			n++
		}
	}
	return n
}

// Resolve works out what the target needs in this game: names the game provides, packages
// to install (with their own dependencies), names nobody can provide, and shadowed game mods.
func (r *Resolver) Resolve(t Target) *Resolution {
	res := &Resolution{
		Target: t.Key, Game: r.Game.Key(), OK: true,
		FromGame: []string{}, Install: []InstallEntry{}, Missing: []Missing{}, Shadowed: []Shadowed{},
		OptionalAvailable: []string{}, OptionalUnavailable: []string{}, Warnings: []string{},
	}
	if t.Type == "game" {
		res.Warnings = append(res.Warnings, "The target is a game; games are played, not installed into games.")
	}
	if GamesWithNonstandardMTGCopies.Has(r.Game.Name) {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s bundles copies of Minetest Game mods that are known "+
			"not to implement the MTG API; like ContentDB, they are ignored here.", r.Game.Key()))
	}
	for _, name := range t.Provides.Intersect(r.gameNames).Sorted() {
		res.Shadowed = append(res.Shadowed, Shadowed{Modname: name, By: t.Key, Note: fmt.Sprintf("replaces the game's own '%s' mod", name)})
	}

	chosen := map[string]string{}
	installed := map[string]*InstallEntry{}
	var order []*InstallEntry

	var need func(name, neededBy string)
	need = func(name, neededBy string) {
		if t.Provides.Has(name) {
			return
		}
		if _, done := chosen[name]; done {
			return
		}
		if r.gameNames.Has(name) {
			chosen[name] = "game"
			res.FromGame = append(res.FromGame, name)
			return
		}
		provider := r.BestProvider(name)
		if provider == nil {
			res.Missing = append(res.Missing, r.explainMissing(name, neededBy))
			chosen[name] = "missing"
			return
		}
		entry := installed[provider.Key()]
		if entry == nil {
			entry = &InstallEntry{Package: provider.Key(), For: []string{}, NeededBy: neededBy,
				Alternatives: len(r.idx.Providers(name)) - 1}
			installed[provider.Key()] = entry
			order = append(order, entry)
			for _, provided := range provider.Provides.Sorted() {
				if r.gameNames.Has(provided) {
					res.Shadowed = append(res.Shadowed, Shadowed{Modname: provided, By: provider.Key(),
						Note: fmt.Sprintf("replaces the game's own '%s' mod", provided)})
				}
				if _, exists := chosen[provided]; !exists {
					chosen[provided] = provider.Key()
				}
			}
			for _, dep := range provider.Depends.Minus(provider.Provides).Sorted() {
				need(dep, provider.Key())
			}
		}
		entry.For = append(entry.For, name)
	}

	for _, name := range t.Depends.Sorted() {
		need(name, t.Key)
	}
	for _, e := range order {
		res.Install = append(res.Install, *e)
	}
	available := r.gameNames.Copy()
	for name := range chosen {
		available.Add(name)
	}
	for name := range t.Provides {
		available.Add(name)
	}
	for _, n := range t.Optional.Sorted() {
		if available.Has(n) {
			res.OptionalAvailable = append(res.OptionalAvailable, n)
		} else {
			res.OptionalUnavailable = append(res.OptionalUnavailable, n)
		}
	}
	res.OK = len(res.Missing) == 0
	return res
}
