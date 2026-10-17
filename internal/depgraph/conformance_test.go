package depgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestConformance compares the port with every precomputed row in the snapshot: the
// committed mini.sqlite by default, or the snapshot named by LUANTHETTI_SNAPSHOT_FILE
// (the nightly workflow sets it to the real one). Precomputed rows are the oracle; the
// port is run with UsePrecomputed off.
func TestConformance(t *testing.T) {
	path := os.Getenv("LUANTHETTI_SNAPSHOT_FILE")
	if path == "" {
		path = filepath.Join("testdata", "mini.sqlite")
	}
	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if !idx.HasPrecomputed() {
		t.Skip("snapshot has no compat_matrix table")
	}
	meta, err := ReadMeta(idx.DB())
	if err != nil {
		t.Fatal(err)
	}
	if meta.RulesVersion != KnownRulesVersion {
		t.Logf("snapshot rules_version %d, port knows %d", meta.RulesVersion, KnownRulesVersion)
	}
	idx.UsePrecomputed = false
	games := map[int64]*Pkg{}
	for _, g := range idx.Games {
		games[g.ID] = g
	}
	compared, mismatches := 0, 0
	for key, want := range idx.pre.compat {
		pkg, game := idx.ByID(key.pkg), games[key.game]
		if pkg == nil || game == nil {
			continue
		}
		refs, err := idx.PackageRefs(pkg)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := idx.Compat(TargetFromPkg(pkg), []*Pkg{game}, refs)
		if err != nil {
			t.Fatal(err)
		}
		got := rows[0]
		got.ContentDBSays = nil
		compared++
		if diff := jsonDiff(got, mustJSON(want)); diff != "" {
			mismatches++
			if mismatches <= 20 {
				t.Errorf("compat %s in %s:\n%s", pkg.Key(), game.Key(), diff)
			}
		}
	}
	resolutions := 0
	for key, want := range idx.pre.resolution {
		pkg, game := idx.ByID(key.pkg), games[key.game]
		if pkg == nil || game == nil {
			continue
		}
		got := NewResolver(idx, game).Resolve(TargetFromPkg(pkg))
		resolutions++
		if diff := jsonDiff(got, want); diff != "" {
			mismatches++
			if mismatches <= 20 {
				t.Errorf("resolve %s in %s:\n%s", pkg.Key(), game.Key(), diff)
			}
		}
	}
	t.Logf("compared %d compat rows and %d resolutions, %d mismatches (ldg %s, rules %d)", compared, resolutions, mismatches, meta.LdgVersion, meta.RulesVersion)
	if compared == 0 {
		t.Fatal("nothing compared")
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestPrecomputedPreferredAndMeta(t *testing.T) {
	idx := openMini(t)
	meta, err := ReadMeta(idx.DB())
	if err != nil {
		t.Fatal(err)
	}
	if meta.SchemaVersion != 1 || meta.RulesVersion != 1 || meta.Packages != 10 || len(meta.PrecomputedGames) != 3 || meta.BuiltAt.IsZero() {
		t.Errorf("%+v", meta)
	}
	if warn, err := CheckSchema(idx.DB(), meta); err != nil || warn != "" {
		t.Errorf("%q %v", warn, err)
	}
	if _, err := CheckSchema(idx.DB(), Meta{SchemaVersion: 99}); err == nil {
		t.Error("unknown schema accepted")
	}
	if _, err := CheckSchema(idx.DB(), Meta{LdgVersion: "0.2.9"}); err == nil {
		t.Error("old ldg accepted")
	}
	if warn, _ := CheckSchema(idx.DB(), Meta{RulesVersion: KnownRulesVersion + 1}); warn == "" {
		t.Error("newer rules should warn")
	}
	// A precomputed answer and the live port agree, and the trivial-row reconstruction works.
	if !idx.HasPrecomputed() {
		t.Fatal("mini.sqlite should carry compat_matrix")
	}
	for _, c := range []struct{ pkg, game string }{{"TenPlus1/ethereal", "minetest_game"}, {"mt-mods/pipeworks", "mineclonia"}, {"someone/fancy_pack", "mineclonia"}, {"mt-mods/xcompat", "mineclone2"}} {
		pre, err := idx.Resolve(c.pkg, c.game)
		if err != nil {
			t.Fatal(err)
		}
		idx.UsePrecomputed = false
		live, _ := idx.Resolve(c.pkg, c.game)
		idx.UsePrecomputed = true
		if diff := jsonDiff(pre, mustJSON(live)); diff != "" {
			t.Errorf("%s in %s: precomputed vs live:\n%s", c.pkg, c.game, diff)
		}
	}
}
