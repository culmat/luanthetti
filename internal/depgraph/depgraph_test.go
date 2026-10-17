package depgraph

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func openMini(t *testing.T) *Index {
	t.Helper()
	idx, err := Open(filepath.Join("testdata", "mini.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

func keys(ps []*Pkg) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Key())
	}
	return out
}

func mustPkg(t *testing.T, idx *Index, spec string) *Pkg {
	t.Helper()
	p, err := idx.Package(spec)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustGame(t *testing.T, idx *Index, spec string) *Pkg {
	t.Helper()
	g, err := idx.Game(spec)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// The assertions of luanti-depgraph's tests/test_resolve.py.

func TestCountsAndAliases(t *testing.T) {
	idx := openMini(t)
	if idx.Len() != 10 {
		t.Errorf("packages = %d", idx.Len())
	}
	if mustPkg(t, idx, "Minetest/minetest_game").Key() != "Luanti/minetest_game" {
		t.Error("alias")
	}
}

func TestGameLookupNormalizes(t *testing.T) {
	idx := openMini(t)
	if mustGame(t, idx, "minetest").Key() != "Luanti/minetest_game" || mustGame(t, idx, "VoxeLibre").Key() != "Wuzzy/mineclone2" {
		t.Error("normalisation")
	}
	if _, err := idx.Game("pipeworks"); err == nil {
		t.Error("a mod is not a game")
	} else if !strings.HasPrefix(err.Error(), "No game 'pipeworks' on ContentDB. Try one of: mineclonia, mineclone2, minetest_game") {
		t.Errorf("message: %v", err)
	}
	if _, err := idx.Package("nothing/here"); err == nil || err.Error() != "No ContentDB package 'nothing/here'. Use author/name, e.g. mt-mods/pipeworks." {
		t.Errorf("message: %v", err)
	}
}

func TestNameOnlyPackageLookupPrefersScore(t *testing.T) {
	idx := openMini(t)
	if mustPkg(t, idx, "xcompat").Key() != "other/xcompat" {
		t.Error("score preference")
	}
	if mustPkg(t, idx, "MT-MODS/Pipeworks").Key() != "mt-mods/pipeworks" {
		t.Error("case-insensitive key")
	}
}

func TestProvidersPreferExactNameThenScore(t *testing.T) {
	idx := openMini(t)
	if got := keys(idx.Providers("xcompat")); !reflect.DeepEqual(got, []string{"other/xcompat", "mt-mods/xcompat"}) {
		t.Errorf("%v", got)
	}
	if got := keys(idx.DefaultGames(10)); !reflect.DeepEqual(got, []string{"Luanti/minetest_game", "ryvnf/mineclonia", "Wuzzy/mineclone2"}) {
		t.Errorf("default games %v", got)
	}
}

func TestResolveInstallsDependencies(t *testing.T) {
	idx := openMini(t)
	res := NewResolver(idx, mustGame(t, idx, "minetest_game")).Resolve(TargetFromPkg(mustPkg(t, idx, "mt-mods/pipeworks")))
	if !res.OK || len(res.Install) != 1 || res.Install[0].Package != "other/xcompat" || !reflect.DeepEqual(res.FromGame, []string{"default"}) {
		t.Errorf("%+v", res)
	}
}

func TestResolveFallsBackToProviderThatWorksInTheGame(t *testing.T) {
	idx := openMini(t)
	res := NewResolver(idx, mustGame(t, idx, "mineclonia")).Resolve(TargetFromPkg(mustPkg(t, idx, "mt-mods/pipeworks")))
	if !res.OK || len(res.Install) != 1 || res.Install[0].Package != "mt-mods/xcompat" {
		t.Errorf("%+v", res)
	}
	// mini-patch.sql: mt-mods/xcompat declares only minetest_game and mineclonia and is
	// game-independent, so it doesn't count in VoxeLibre.
	res = NewResolver(idx, mustGame(t, idx, "mineclone2")).Resolve(TargetFromPkg(mustPkg(t, idx, "mt-mods/pipeworks")))
	if res.OK || res.Missing[0].Modname != "xcompat" || res.Missing[0].Candidates[1].Problem != "dependency cycle" {
		t.Errorf("%+v", res)
	}
}

func TestResolveExplainsGameOnlyDependencies(t *testing.T) {
	idx := openMini(t)
	res := NewResolver(idx, mustGame(t, idx, "mineclonia")).Resolve(TargetFromPkg(mustPkg(t, idx, "TenPlus1/ethereal")))
	if res.OK {
		t.Fatal("should fail")
	}
	names := map[string]Missing{}
	for _, m := range res.Missing {
		names[m.Modname] = m
	}
	if len(names) != 2 || !reflect.DeepEqual(names["default"].OnlyInGames, []string{"Luanti/minetest_game"}) {
		t.Errorf("%+v", res.Missing)
	}
	if _, ok := names["stairs"]; !ok {
		t.Error("stairs missing")
	}
}

func TestCyclesTerminate(t *testing.T) {
	idx := openMini(t)
	res := NewResolver(idx, mustGame(t, idx, "minetest_game")).Resolve(TargetFromPkg(mustPkg(t, idx, "someone/cycle_a")))
	if res.OK {
		t.Error("cycle_a should not resolve")
	}
}

func TestLocalModNamedLikeAGameModIsFlagged(t *testing.T) {
	idx := openMini(t)
	dir := filepath.Join(t.TempDir(), "default")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "init.lua"), nil, 0o644)
	res, err := idx.Resolve(dir, "minetest_game")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Shadowed) != 1 || res.Shadowed[0].Modname != "default" || res.Target != "local:default" {
		t.Errorf("%+v", res)
	}
}

func TestCompatMatrix(t *testing.T) {
	idx := openMini(t)
	rep, err := idx.CompatReport("TenPlus1/ethereal", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]CompatRow{}
	for _, r := range rep.Games {
		rows[r.Game] = r
	}
	if rows["Luanti/minetest_game"].Status != "ok" || rows["ryvnf/mineclonia"].Status != "unsatisfiable" {
		t.Errorf("%+v", rows)
	}
	if c := rows["Luanti/minetest_game"].ContentDBSays; c == nil || !c.Supports || c.Confidence != 1 {
		t.Errorf("claim %+v", c)
	}
	// pipeworks' extra_part uses mcl_core undeclared (mini-patch.sql): risky in Minetest Game.
	rep, _ = idx.CompatReport("mt-mods/pipeworks", []string{"minetest", "mineclonia"})
	if !rep.AnalyzedSources || rep.Games[0].Status != "risky" || !reflect.DeepEqual(rep.Games[0].UndeclaredMissing, []string{"core_part (global)", "mcl_core (global)"}) {
		t.Errorf("%+v", rep.Games[0])
	}
	r := NewResolver(idx, mustGame(t, idx, "minetest_game"))
	if s, _ := idx.Status(r, mustPkg(t, idx, "mt-mods/pipeworks")); s != 'r' {
		t.Errorf("status %c", s)
	}
	if s, _ := idx.Status(r, mustPkg(t, idx, "TenPlus1/ethereal")); s != 'o' {
		t.Errorf("status %c", s)
	}
	if s, _ := idx.Status(r, mustPkg(t, idx, "someone/cycle_a")); s != 'u' {
		t.Errorf("status %c", s)
	}
}

func TestNameCheck(t *testing.T) {
	idx := openMini(t)
	if idx.NameCheck("pipeworks", "").Available || !idx.NameCheck("brand_new_mod", "").Available || idx.NameCheck("Bad-Name", "").Valid {
		t.Error("availability")
	}
	if got := idx.NameCheck("default", "").BundledInGames; !reflect.DeepEqual(got, []string{"Luanti/minetest_game"}) {
		t.Errorf("%v", got)
	}
}

func TestSearch(t *testing.T) {
	idx := openMini(t)
	hits := idx.Search("xcompat", "mod", 20)
	if len(hits) != 2 || hits[0].Package != "other/xcompat" {
		t.Errorf("%+v", hits)
	}
	if hits := idx.Search("pipe", "", 20); len(hits) != 1 || hits[0].Provides[0] != "pipeworks" {
		t.Errorf("%+v", hits)
	}
}

// Golden comparison against `ldg ... --json` output committed in testdata/golden.

func goldenCases(t *testing.T, idx *Index) map[string]func() (any, error) {
	rs := func(spec, game string) func() (any, error) {
		return func() (any, error) { return idx.Resolve(spec, game) }
	}
	cp := func(spec string) func() (any, error) {
		return func() (any, error) { return idx.CompatReport(spec, nil) }
	}
	return map[string]func() (any, error){
		"compat_pipeworks":                 cp("mt-mods/pipeworks"),
		"compat_ethereal":                  cp("TenPlus1/ethereal"),
		"compat_xcompat_other":             cp("other/xcompat"),
		"compat_xcompat_mtmods":            cp("mt-mods/xcompat"),
		"compat_cycle_a":                   cp("someone/cycle_a"),
		"resolve_pipeworks_mtg":            rs("mt-mods/pipeworks", "minetest_game"),
		"resolve_pipeworks_mineclonia":     rs("mt-mods/pipeworks", "mineclonia"),
		"resolve_pipeworks_voxelibre":      rs("mt-mods/pipeworks", "VoxeLibre"),
		"resolve_ethereal_mineclonia":      rs("TenPlus1/ethereal", "mineclonia"),
		"resolve_ethereal_mtg":             rs("TenPlus1/ethereal", "minetest"),
		"resolve_cycle_a_mtg":              rs("someone/cycle_a", "minetest_game"),
		"resolve_xcompat_other_mineclonia": rs("other/xcompat", "mineclonia"),
		"resolve_game_target":              rs("ryvnf/mineclonia", "minetest_game"),
		"providers_xcompat":                func() (any, error) { return idx.ProvidersReport("xcompat"), nil },
		"providers_default":                func() (any, error) { return idx.ProvidersReport("default"), nil },
		"providers_nothing":                func() (any, error) { return idx.ProvidersReport("nonexistent_name_xyz"), nil },
		"search_pipe":                      func() (any, error) { return idx.Search("pipe", "", 20), nil },
		"search_xcompat_mod":               func() (any, error) { return idx.Search("xcompat", "mod", 20), nil },
		"namecheck_pipeworks":              func() (any, error) { return idx.NameCheck("pipeworks", ""), nil },
		"namecheck_default":                func() (any, error) { return idx.NameCheck("default", ""), nil },
		"namecheck_free":                   func() (any, error) { return idx.NameCheck("brand_new_mod", ""), nil },
		"namecheck_bad":                    func() (any, error) { return idx.NameCheck("Bad-Name", ""), nil },
	}
}

func TestGolden(t *testing.T) {
	idx := openMini(t)
	cases := goldenCases(t, idx)
	files, _ := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if len(files) != len(cases) {
		t.Fatalf("%d golden files, %d cases", len(files), len(cases))
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		fn, ok := cases[name]
		if !ok {
			t.Errorf("no case for golden %s", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			want, _ := os.ReadFile(f)
			got, err := fn()
			if err != nil {
				t.Fatal(err)
			}
			if diff := jsonDiff(got, want); diff != "" {
				t.Errorf("mismatch with ldg:\n%s", diff)
			}
		})
	}
}

// jsonDiff compares a Go value with ldg's JSON after normalising both through encoding/json.
func jsonDiff(got any, want []byte) string {
	gotBytes, err := json.Marshal(got)
	if err != nil {
		return err.Error()
	}
	var g, w any
	json.Unmarshal(gotBytes, &g)
	if err := json.Unmarshal(want, &w); err != nil {
		return "golden: " + err.Error()
	}
	var diffs []string
	walk("$", g, w, &diffs)
	return strings.Join(diffs, "\n")
}

func walk(path string, g, w any, diffs *[]string) {
	switch wv := w.(type) {
	case map[string]any:
		gv, ok := g.(map[string]any)
		if !ok {
			*diffs = append(*diffs, path+": want object, got "+typeName(g))
			return
		}
		for k, wk := range wv {
			gk, ok := gv[k]
			if !ok {
				*diffs = append(*diffs, path+"."+k+": missing")
				continue
			}
			walk(path+"."+k, gk, wk, diffs)
		}
		for k := range gv {
			if _, ok := wv[k]; !ok {
				*diffs = append(*diffs, path+"."+k+": unexpected key")
			}
		}
	case []any:
		gv, ok := g.([]any)
		if !ok {
			*diffs = append(*diffs, path+": want array, got "+typeName(g))
			return
		}
		if len(gv) != len(wv) {
			*diffs = append(*diffs, path+": want "+itoa(len(wv))+" items, got "+itoa(len(gv)))
			return
		}
		for i := range wv {
			walk(path+"["+itoa(i)+"]", gv[i], wv[i], diffs)
		}
	case float64:
		gf, ok := g.(float64)
		if !ok || math.Abs(gf-wv) > 0.051 {
			*diffs = append(*diffs, path+": want "+ftoa(wv)+", got "+ftoa(g))
		}
	default:
		if !reflect.DeepEqual(g, w) {
			*diffs = append(*diffs, path+": want "+ftoa(w)+", got "+ftoa(g))
		}
	}
}

func typeName(v any) string { return reflect.TypeOf(v).String() }
func itoa(i int) string     { b, _ := json.Marshal(i); return string(b) }
func ftoa(v any) string     { b, _ := json.Marshal(v); return string(b) }
