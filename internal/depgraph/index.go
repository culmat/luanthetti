// Package depgraph reads luanti-depgraph's SQLite snapshot and answers "can package X run in
// game G, and what has to be installed?". It is a port of ldg's resolve.py and the parts of
// queries.py Luanthetti needs; see doc.go for the deviations. Precomputed answers in the
// snapshot (compat_matrix, resolution tables) are preferred when present.
package depgraph

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

// Pkg is one ContentDB package with its mod names.
type Pkg struct {
	ID        int64
	Author    string
	Name      string
	Type      string // mod | game | txp
	Title     string
	Score     float64
	Downloads int64
	Provides  Set
	Depends   Set
	Optional  Set
}

// Key is "author/name".
func (p *Pkg) Key() string { return p.Author + "/" + p.Name }

// Summary is ldg's `Pkg.summary()`.
func (p *Pkg) Summary() Summary {
	return Summary{Package: p.Key(), Type: p.Type, Title: p.Title, Score: round1(p.Score), Downloads: p.Downloads}
}

// Index holds every package with provides and dependencies in memory, like ldg's Index.
type Index struct {
	conn *sql.DB

	order       []*Pkg // id ascending: Python's dict insertion order in ldg
	byID        map[int64]*Pkg
	byKey       map[string]*Pkg // "author/name" plus aliases
	byKeyLower  map[string]*Pkg
	byName      map[string][]*Pkg
	ProvidersOf map[string][]*Pkg
	DependersOf map[string][]*Pkg
	// Games sorted by score, descending (stable).
	Games []*Pkg

	declaredSupport     map[int64]map[int64]bool // package id → game ids with confidence > 5 and supports
	declaredUnsupported map[int64]map[int64]bool
	supportsAll         map[int64]bool
	independent         map[int64]bool

	HasRefs bool // the snapshot has the mod/ref tables (static analysis)
	// UsePrecomputed makes Compat and Resolve return the snapshot's precomputed rows when
	// present. Tests turn it off to exercise the port.
	UsePrecomputed bool
	pre            *precomputed

	detailsOnce sync.Once
	details     map[int64]Details
	hasTags     bool
	mu          sync.Mutex // guards resolvers
	indepMu     sync.Mutex // guards independent
	resolvers   map[int64]*Resolver
}

// ResolverFor returns a shared, memoising resolver for a game. Safe for concurrent use of
// different games; callers resolving in the same game concurrently must serialise themselves
// (the app layer runs one operation at a time).
func (idx *Index) ResolverFor(game *Pkg) *Resolver {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.resolvers == nil {
		idx.resolvers = map[int64]*Resolver{}
	}
	r := idx.resolvers[game.ID]
	if r == nil {
		r = NewResolver(idx, game)
		idx.resolvers[game.ID] = r
	}
	return r
}

// Open opens a snapshot read-only and loads the index.
func Open(path string) (*Index, error) {
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	idx, err := Load(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return idx, nil
}

// Load builds the index from an open database.
func Load(conn *sql.DB) (*Index, error) {
	idx := &Index{
		conn:                conn,
		byID:                map[int64]*Pkg{},
		byKey:               map[string]*Pkg{},
		byKeyLower:          map[string]*Pkg{},
		byName:              map[string][]*Pkg{},
		ProvidersOf:         map[string][]*Pkg{},
		DependersOf:         map[string][]*Pkg{},
		declaredSupport:     map[int64]map[int64]bool{},
		declaredUnsupported: map[int64]map[int64]bool{},
		supportsAll:         map[int64]bool{},
		independent:         map[int64]bool{},
		UsePrecomputed:      true,
	}
	rows, err := conn.Query("SELECT id, author, name, type, title, score, downloads FROM package ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("depgraph: %w", err)
	}
	for rows.Next() {
		p := &Pkg{Provides: Set{}, Depends: Set{}, Optional: Set{}}
		var title sql.NullString
		var score sql.NullFloat64
		var downloads sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Author, &p.Name, &p.Type, &title, &score, &downloads); err != nil {
			rows.Close()
			return nil, err
		}
		p.Title, p.Score, p.Downloads = title.String, score.Float64, downloads.Int64
		idx.byID[p.ID] = p
		idx.order = append(idx.order, p)
	}
	rows.Close()
	if err := idx.each("SELECT package_id, modname FROM provides ORDER BY rowid", func(sc scanner) error {
		var pid int64
		var name string
		if err := sc.Scan(&pid, &name); err != nil {
			return err
		}
		if p := idx.byID[pid]; p != nil {
			p.Provides.Add(name)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := idx.each("SELECT package_id, modname, optional FROM dep ORDER BY rowid", func(sc scanner) error {
		var pid int64
		var name string
		var optional int
		if err := sc.Scan(&pid, &name, &optional); err != nil {
			return err
		}
		if p := idx.byID[pid]; p != nil {
			if optional != 0 {
				p.Optional.Add(name)
			} else {
				p.Depends.Add(name)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	for _, p := range idx.order {
		idx.byKey[p.Key()] = p
		idx.byKeyLower[strings.ToLower(p.Key())] = p
	}
	if err := idx.each("SELECT alias, package_id FROM package_alias ORDER BY rowid", func(sc scanner) error {
		var alias string
		var pid int64
		if err := sc.Scan(&alias, &pid); err != nil {
			return err
		}
		if p := idx.byID[pid]; p != nil {
			if _, exists := idx.byKey[alias]; !exists {
				idx.byKey[alias] = p
				idx.byKeyLower[strings.ToLower(alias)] = p
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	for _, p := range idx.order {
		idx.byName[p.Name] = append(idx.byName[p.Name], p)
		for _, m := range p.Provides.Sorted() {
			idx.ProvidersOf[m] = append(idx.ProvidersOf[m], p)
		}
		for _, m := range p.Depends.Union(p.Optional).Sorted() {
			idx.DependersOf[m] = append(idx.DependersOf[m], p)
		}
		if p.Type == "game" {
			idx.Games = append(idx.Games, p)
		}
	}
	sort.SliceStable(idx.Games, func(i, j int) bool { return idx.Games[i].Score > idx.Games[j].Score })
	if err := idx.each("SELECT package_id, game_id, supports FROM game_support WHERE confidence > 5", func(sc scanner) error {
		var pid, gid int64
		var supports int
		if err := sc.Scan(&pid, &gid, &supports); err != nil {
			return err
		}
		table := idx.declaredUnsupported
		if supports != 0 {
			table = idx.declaredSupport
		}
		if table[pid] == nil {
			table[pid] = map[int64]bool{}
		}
		table[pid][gid] = true
		return nil
	}); err != nil {
		return nil, err
	}
	if err := idx.each("SELECT id FROM package WHERE supports_all_games = 1", func(sc scanner) error {
		var id int64
		if err := sc.Scan(&id); err != nil {
			return err
		}
		idx.supportsAll[id] = true
		return nil
	}); err != nil {
		return nil, err
	}
	idx.HasRefs = idx.hasTable("ref") && idx.hasTable("mod")
	if err := idx.loadPrecomputed(); err != nil {
		return nil, err
	}
	return idx, nil
}

type scanner interface{ Scan(dest ...any) error }

func (idx *Index) each(query string, fn func(scanner) error) error {
	rows, err := idx.conn.Query(query)
	if err != nil {
		return fmt.Errorf("depgraph: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (idx *Index) hasTable(name string) bool {
	var n int
	err := idx.conn.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n)
	return err == nil && n > 0
}

// DB exposes the underlying connection for metadata queries.
func (idx *Index) DB() *sql.DB { return idx.conn }

// Close closes the database.
func (idx *Index) Close() error { return idx.conn.Close() }

// Len is the number of packages.
func (idx *Index) Len() int { return len(idx.order) }

// All returns every package in id order.
func (idx *Index) All() []*Pkg { return idx.order }

// ByID looks a package up by its snapshot id.
func (idx *Index) ByID(id int64) *Pkg { return idx.byID[id] }

// norm is ldg's `_norm`: trim, lower, strip a `_game` suffix, then delete spaces.
func norm(v string) string {
	return strings.ReplaceAll(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), "_game"), " ", "")
}

// Package finds a package by "author/name" (also aliases, case-insensitively) or by bare name,
// preferring non-texture-packs and then the best score.
func (idx *Index) Package(spec string) (*Pkg, error) {
	spec = strings.Trim(strings.TrimSpace(spec), "/")
	if p, ok := idx.byKey[spec]; ok {
		return p, nil
	}
	if p, ok := idx.byKeyLower[strings.ToLower(spec)]; ok {
		return p, nil
	}
	matches := idx.byName[spec]
	if matches == nil {
		matches = idx.byName[strings.ToLower(spec)]
	}
	if len(matches) > 0 {
		best := matches[0]
		for _, p := range matches[1:] {
			if rank(p) > rank(best) || (rank(p) == rank(best) && p.Score > best.Score) {
				best = p
			}
		}
		return best, nil
	}
	return nil, &NotFoundError{Msg: fmt.Sprintf("No ContentDB package '%s'. Use author/name, e.g. mt-mods/pipeworks.", spec)}
}

func rank(p *Pkg) int {
	if p.Type != "txp" {
		return 1
	}
	return 0
}

// Alternatives are other packages with the same name.
func (idx *Index) Alternatives(pkg *Pkg) []*Pkg {
	var out []*Pkg
	for _, p := range idx.byName[pkg.Name] {
		if p.ID != pkg.ID {
			out = append(out, p)
		}
	}
	return out
}

// Game finds a game by key, name, title or alias. "minetest" means Minetest Game.
func (idx *Index) Game(spec string) (*Pkg, error) {
	if p, ok := idx.byKey[spec]; ok && p.Type == "game" {
		return p, nil
	}
	want := norm(spec)
	for _, g := range idx.Games {
		if want == norm(g.Name) || want == norm(g.Title) || want == norm(g.Key()) {
			return g, nil
		}
	}
	// ldg retries "minetest" as "minetest_game" here; both normalise to "minetest", so the
	// loop above already covers it and the retry can only recurse. Omitted on purpose.
	names := make([]string, 0, 12)
	for _, g := range idx.Games {
		if len(names) == 12 {
			break
		}
		names = append(names, g.Name)
	}
	return nil, &NotFoundError{Msg: fmt.Sprintf("No game '%s' on ContentDB. Try one of: %s", spec, strings.Join(names, ", "))}
}

// DefaultGames returns the reference games that exist, then the best-scored others, up to n.
func (idx *Index) DefaultGames(n int) []*Pkg {
	var chosen []*Pkg
	for _, ref := range ReferenceGames {
		for _, g := range idx.Games {
			if norm(g.Name) == norm(ref) {
				if p, err := idx.Game(ref); err == nil {
					chosen = append(chosen, p)
				}
				break
			}
		}
	}
	for _, g := range idx.Games {
		if len(chosen) >= n {
			break
		}
		if !containsPkg(chosen, g) {
			chosen = append(chosen, g)
		}
	}
	return chosen
}

func containsPkg(list []*Pkg, p *Pkg) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

// Providers lists the packages providing a mod name in the installer's preference: the
// non-game package named like the mod, other non-game providers by score, then games.
func (idx *Index) Providers(modname string) []*Pkg {
	src := idx.ProvidersOf[modname]
	out := make([]*Pkg, len(src))
	copy(out, src)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Type == "game") != (b.Type == "game") {
			return a.Type != "game"
		}
		if (a.Name != modname) != (b.Name != modname) {
			return a.Name == modname
		}
		return a.Score > b.Score
	})
	return out
}

// GameIndependent reports whether the package's hard dependencies can be met by standalone
// mods alone (resolved against a game that provides nothing, ignoring declared support).
func (idx *Index) GameIndependent(pkg *Pkg) bool {
	idx.indepMu.Lock()
	v, ok := idx.independent[pkg.ID]
	idx.indepMu.Unlock()
	if ok {
		return v
	}
	nowhere := &Pkg{ID: -1, Type: "game", Provides: Set{}, Depends: Set{}, Optional: Set{}}
	r := newResolver(idx, nowhere, false)
	ok, _ = r.satisfiable(pkg, nil)
	idx.indepMu.Lock()
	idx.independent[pkg.ID] = ok
	idx.indepMu.Unlock()
	return ok
}
