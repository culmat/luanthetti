package depgraph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SupportedSchemaVersions are the snapshot schema versions this build understands.
var SupportedSchemaVersions = map[int]bool{1: true}

// KnownRulesVersion is the ldg rules_version this port was checked against.
const KnownRulesVersion = 1

// MinLdgVersion is the oldest ldg that writes a snapshot this package accepts.
const MinLdgVersion = "0.3.1"

// Meta is the snapshot's self-description from its meta table.
type Meta struct {
	BuiltAt          time.Time      `json:"built_at"`
	LdgVersion       string         `json:"ldg_version"`
	LdgCommit        string         `json:"ldg_commit,omitempty"`
	SchemaVersion    int            `json:"schema_version"` // 0 when the snapshot predates the contract
	RulesVersion     int            `json:"rules_version"`
	Packages         int            `json:"packages"`
	Games            int            `json:"games"`
	Mods             int            `json:"mods"`
	Txps             int            `json:"txps"`
	CorpusPackages   int            `json:"corpus_packages"`
	Refs             int            `json:"refs"`
	Details          bool           `json:"details"`
	PrecomputedGames []string       `json:"precomputed_games"`
	Raw              map[string]any `json:"-"`
}

// ReadMeta parses the meta table (values are JSON-encoded, as ldg writes them).
func ReadMeta(conn *sql.DB) (Meta, error) {
	rows, err := conn.Query("SELECT key, value FROM meta")
	if err != nil {
		return Meta{}, fmt.Errorf("depgraph: meta: %w", err)
	}
	defer rows.Close()
	m := Meta{Raw: map[string]any{}}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Meta{}, err
		}
		var parsed any
		if json.Unmarshal([]byte(v), &parsed) != nil {
			parsed = v
		}
		m.Raw[k] = parsed
	}
	if err := rows.Err(); err != nil {
		return Meta{}, err
	}
	if s, ok := m.Raw["built_at"].(string); ok {
		m.BuiltAt, _ = time.Parse(time.RFC3339, s)
	}
	m.LdgVersion, _ = m.Raw["ldg_version"].(string)
	m.LdgCommit, _ = m.Raw["ldg_commit"].(string)
	m.SchemaVersion = intOf(m.Raw["schema_version"])
	m.RulesVersion = intOf(m.Raw["rules_version"])
	m.Packages, m.Games, m.Mods, m.Txps = intOf(m.Raw["packages"]), intOf(m.Raw["games"]), intOf(m.Raw["mods"]), intOf(m.Raw["txps"])
	m.CorpusPackages, m.Refs = intOf(m.Raw["corpus_packages"]), intOf(m.Raw["refs"])
	switch d := m.Raw["details"].(type) {
	case bool:
		m.Details = d
	case string:
		m.Details = d != ""
	}
	if list, ok := m.Raw["precomputed_games"].([]any); ok {
		for _, g := range list {
			if s, ok := g.(string); ok {
				m.PrecomputedGames = append(m.PrecomputedGames, s)
			}
		}
	}
	return m, nil
}

func intOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

// CheckSchema verifies that the database has what the port needs. It returns an error for a
// snapshot that is too old or has an unknown schema_version, and a warning string (or "") for
// a newer rules_version than this port knows.
func CheckSchema(conn *sql.DB, m Meta) (warning string, err error) {
	if m.SchemaVersion != 0 && !SupportedSchemaVersions[m.SchemaVersion] {
		return "", fmt.Errorf("snapshot schema_version %d is not supported by this luanthetti (knows %v); upgrade luanthetti", m.SchemaVersion, keysOf(SupportedSchemaVersions))
	}
	if m.LdgVersion != "" && versionLess(m.LdgVersion, MinLdgVersion) {
		return "", fmt.Errorf("snapshot was built by luanti-depgraph %s; at least %s is required", m.LdgVersion, MinLdgVersion)
	}
	required := map[string][]string{
		"meta":          {"key", "value"},
		"package":       {"id", "author", "name", "type", "title", "short_description", "release_id", "downloads", "score"},
		"package_alias": {"alias", "package_id"},
		"provides":      {"package_id", "modname"},
		"dep":           {"package_id", "modname", "optional"},
		"game_support":  {"package_id", "game_id", "supports", "confidence"},
	}
	for table, cols := range required {
		have, err := columns(conn, table)
		if err != nil {
			return "", err
		}
		if len(have) == 0 {
			return "", fmt.Errorf("snapshot lacks table %s", table)
		}
		for _, c := range cols {
			if !have[c] {
				return "", fmt.Errorf("snapshot table %s lacks column %s", table, c)
			}
		}
	}
	if m.RulesVersion > KnownRulesVersion {
		warning = fmt.Sprintf("snapshot uses resolution rules version %d; this luanthetti knows version %d and prefers the snapshot's precomputed answers", m.RulesVersion, KnownRulesVersion)
	}
	return warning, nil
}

func columns(conn *sql.DB, table string) (map[string]bool, error) {
	rows, err := conn.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func keysOf(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	return out
}

// versionLess compares dotted numeric versions ("0.3.1" < "0.3.2").
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(strings.TrimRight(as[i], "abcdefghijklmnopqrstuvwxyz-"))
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

type pairKey struct{ pkg, game int64 }

// precomputed holds the snapshot's compat_matrix and resolution rows.
type precomputed struct {
	compat     map[pairKey]CompatRow
	resolution map[pairKey][]byte
}

// loadPrecomputed reads compat_matrix and resolution when the tables exist.
func (idx *Index) loadPrecomputed() error {
	if !idx.hasTable("compat_matrix") {
		return nil
	}
	pc := &precomputed{compat: map[pairKey]CompatRow{}, resolution: map[pairKey][]byte{}}
	err := idx.each("SELECT package_id, game_id, status, missing, install, undeclared_missing, optional_available FROM compat_matrix", func(sc scanner) error {
		var pid, gid int64
		var status, missing, install, undeclared, optional string
		if err := sc.Scan(&pid, &gid, &status, &missing, &install, &undeclared, &optional); err != nil {
			return err
		}
		game := idx.byID[gid]
		if game == nil {
			return nil
		}
		row := CompatRow{Game: game.Key(), Title: game.Title, Status: status,
			Missing: jsonStrings(missing), Install: jsonStrings(install),
			UndeclaredMissing: jsonStrings(undeclared), OptionalAvailable: jsonStrings(optional)}
		pc.compat[pairKey{pid, gid}] = row
		return nil
	})
	if err != nil {
		return err
	}
	if idx.hasTable("resolution") {
		err = idx.each("SELECT package_id, game_id, json FROM resolution", func(sc scanner) error {
			var pid, gid int64
			var js []byte
			if err := sc.Scan(&pid, &gid, &js); err != nil {
				return err
			}
			pc.resolution[pairKey{pid, gid}] = js
			return nil
		})
		if err != nil {
			return err
		}
	}
	idx.pre = pc
	return nil
}

func jsonStrings(s string) []string {
	var out []string
	if json.Unmarshal([]byte(s), &out) != nil || out == nil {
		return []string{}
	}
	return out
}

// HasPrecomputed reports whether the snapshot carries compat_matrix rows.
func (idx *Index) HasPrecomputed() bool { return idx.pre != nil && len(idx.pre.compat) > 0 }

// PrecomputedCompat returns the snapshot's row for a package in a game, without contentdb_says.
func (idx *Index) PrecomputedCompat(p, game *Pkg) (CompatRow, bool) {
	if idx.pre == nil || !idx.UsePrecomputed {
		return CompatRow{}, false
	}
	row, ok := idx.pre.compat[pairKey{p.ID, game.ID}]
	return row, ok
}

// PrecomputedResolution returns the snapshot's full resolve answer when it was stored; a
// package covered by compat_matrix with no resolution row had a trivial answer, which is
// rebuilt from the matrix row.
func (idx *Index) PrecomputedResolution(p, game *Pkg) (*Resolution, bool) {
	if idx.pre == nil || !idx.UsePrecomputed {
		return nil, false
	}
	key := pairKey{p.ID, game.ID}
	if js, ok := idx.pre.resolution[key]; ok {
		var res Resolution
		if json.Unmarshal(js, &res) == nil {
			fillEmpty(&res)
			return &res, true
		}
		return nil, false
	}
	row, ok := idx.pre.compat[key]
	if !ok || row.Status == "unsatisfiable" || len(row.Install) > 0 {
		return nil, false
	}
	// Trivial: nothing to install, nothing missing, nothing shadowed, no warnings.
	t := TargetFromPkg(p)
	names := game.Provides
	if GamesWithNonstandardMTGCopies.Has(game.Name) {
		return nil, false // such games always carry a warning, so a row would have been stored
	}
	res := &Resolution{Target: t.Key, Game: game.Key(), OK: true, FromGame: []string{}, Install: []InstallEntry{},
		Missing: []Missing{}, Shadowed: []Shadowed{}, OptionalAvailable: row.OptionalAvailable,
		OptionalUnavailable: []string{}, Warnings: []string{}}
	for _, n := range t.Depends.Sorted() {
		if names.Has(n) {
			res.FromGame = append(res.FromGame, n)
		}
	}
	for _, n := range t.Optional.Sorted() {
		if !contains(row.OptionalAvailable, n) {
			res.OptionalUnavailable = append(res.OptionalUnavailable, n)
		}
	}
	return res, true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func fillEmpty(r *Resolution) {
	r.FromGame = strs(r.FromGame)
	r.OptionalAvailable = strs(r.OptionalAvailable)
	r.OptionalUnavailable = strs(r.OptionalUnavailable)
	r.Warnings = strs(r.Warnings)
	if r.Install == nil {
		r.Install = []InstallEntry{}
	}
	for i := range r.Install {
		r.Install[i].For = strs(r.Install[i].For)
	}
	if r.Missing == nil {
		r.Missing = []Missing{}
	}
	for i := range r.Missing {
		r.Missing[i].OnlyInGames = strs(r.Missing[i].OnlyInGames)
		if r.Missing[i].Candidates == nil {
			r.Missing[i].Candidates = []Candidate{}
		}
	}
	if r.Shadowed == nil {
		r.Shadowed = []Shadowed{}
	}
}
