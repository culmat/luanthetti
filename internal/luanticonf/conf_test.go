package luanticonf

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const fixtures = "../../testdata/packages"

func TestParseConfMultilineAndComments(t *testing.T) {
	conf := ParseConf("# comment\nname = x\ndescription = \"\"\"line one\nline two\"\"\"\ndepends = a, b ,c\n")
	if conf["name"] != "x" {
		t.Errorf("name = %q", conf["name"])
	}
	if conf["description"] != "line one\nline two" {
		t.Errorf("description = %q", conf["description"])
	}
	if got := SplitNames(conf["depends"]); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("depends = %v", got)
	}
	// CRLF files, duplicate keys (last wins), lines without "=".
	conf = ParseConf("name = a\r\nname = b\r\nnonsense\r\n")
	if conf["name"] != "b" || len(conf) != 1 {
		t.Errorf("crlf/duplicates: %v", conf)
	}
}

func TestSplitGamesStripsGameSuffix(t *testing.T) {
	if got := SplitGames("*, minetest_game, mineclonia"); !reflect.DeepEqual(got, []string{"*", "minetest", "mineclonia"}) {
		t.Errorf("got %v", got)
	}
	if got := SplitNames(""); got != nil {
		t.Errorf("empty: %v", got)
	}
}

func TestScanModWithModConf(t *testing.T) {
	pkg, err := ScanPackage(filepath.Join(fixtures, "undeclared_mod"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Type != "mod" || len(pkg.Mods) != 1 || pkg.Mods[0].Name != "undeclared_mod" {
		t.Fatalf("%+v", pkg)
	}
	if !reflect.DeepEqual(pkg.HardDepends(), map[string]bool{"xcompat": true}) {
		t.Errorf("hard = %v", pkg.HardDepends())
	}
	if !reflect.DeepEqual(pkg.OptionalDepends(), map[string]bool{"stairs": true}) {
		t.Errorf("optional = %v", pkg.OptionalDepends())
	}
	if pkg.Mods[0].Path != "" {
		t.Errorf("root mod path = %q", pkg.Mods[0].Path)
	}
}

func TestScanLegacyDependsTxt(t *testing.T) {
	pkg, err := ScanPackage(filepath.Join(fixtures, "legacy_mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod := pkg.Mods[0]
	if !mod.LegacyDependsTxt || !reflect.DeepEqual(mod.Depends, []string{"default"}) || !reflect.DeepEqual(mod.OptionalDepends, []string{"farming"}) {
		t.Errorf("%+v", mod)
	}
}

func TestScanModpackDropsSelfProvidedNames(t *testing.T) {
	pkg, err := ScanPackage(filepath.Join(fixtures, "my_modpack"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Type != "modpack" {
		t.Fatalf("type %q", pkg.Type)
	}
	if !reflect.DeepEqual(pkg.Provides(), map[string]bool{"core_part": true, "extra_part": true}) {
		t.Errorf("provides = %v", pkg.Provides())
	}
	if !reflect.DeepEqual(pkg.HardDepends(), map[string]bool{"default": true}) {
		t.Errorf("hard = %v", pkg.HardDepends())
	}
	if !reflect.DeepEqual(pkg.SupportedGames, []string{"minetest", "mineclonia"}) {
		t.Errorf("supported = %v", pkg.SupportedGames)
	}
	if pkg.Mods[0].Path != "core_part" {
		t.Errorf("path = %q", pkg.Mods[0].Path)
	}
}

func TestScanGameWithNestedModpack(t *testing.T) {
	pkg, err := ScanPackage(filepath.Join(fixtures, "tiny_game"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Type != "game" || pkg.Name != "tiny" {
		t.Fatalf("%q %q", pkg.Type, pkg.Name)
	}
	if !reflect.DeepEqual(pkg.Provides(), map[string]bool{"tiny_core": true, "tiny_extra": true}) {
		t.Errorf("provides = %v", pkg.Provides())
	}
	if len(pkg.HardDepends()) != 0 {
		t.Errorf("hard = %v", pkg.HardDepends())
	}
}

func TestFindContentRootUnwrapsSingleDir(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "repo-main", "mymod")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(inner, "init.lua"), nil, 0o644)
	pkg, err := ScanPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Type != "mod" || pkg.Name != "mymod" {
		t.Errorf("%+v", pkg)
	}
	// Two sub-directories: not unwrapped, and with mods inside it counts as a modpack.
	os.MkdirAll(filepath.Join(dir, "other"), 0o755)
	os.WriteFile(filepath.Join(dir, "other", "init.lua"), nil, 0o644)
	pkg, _ = ScanPackage(dir)
	// Only "other" is a mod at this level ("repo-main" is a plain folder), so, like ldg, the
	// root counts as a modpack of the mods directly below it.
	if pkg.Type != "modpack" || len(pkg.Mods) != 1 || pkg.Mods[0].Name != "other" {
		t.Errorf("%q %+v", pkg.Type, pkg.Mods)
	}
	if _, err := ScanPackage(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing dir should fail")
	}
}

func TestEditPreservesCommentsAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mod.conf")
	orig := "# my mod\nname = ethereal\ndescription = \"\"\"Line one\nline two\"\"\"\n\ndepends = default\n"
	os.WriteFile(path, []byte(orig), 0o644)
	err := Edit(path, map[string]string{"author": "TenPlus1", "release": "31245", "name": "ethereal", "title": "Ethereal NG"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "# my mod\nname = ethereal\ndescription = \"\"\"Line one\nline two\"\"\"\n\ndepends = default\nauthor = TenPlus1\nrelease = 31245\ntitle = Ethereal NG\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// Replace the multi-line value in place; the rest stays.
	if err := Edit(path, map[string]string{"description": "Short", "release": "31300"}); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	want = "# my mod\nname = ethereal\ndescription = Short\n\ndepends = default\nauthor = TenPlus1\nrelease = 31300\ntitle = Ethereal NG\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if conf := ReadConf(path); conf["release"] != "31300" || conf["description"] != "Short" {
		t.Errorf("reparse: %v", conf)
	}
}

func TestEditCreatesFileAndKeepsCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "texture_pack.conf")
	if err := Edit(path, map[string]string{"name": "soothing32", "author": "Zughy"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "author = Zughy\nname = soothing32\n" {
		t.Errorf("created: %q", got)
	}
	crlf := filepath.Join(dir, "game.conf")
	os.WriteFile(crlf, []byte("title = X\r\nname = y\r\n"), 0o644)
	Edit(crlf, map[string]string{"release": "7", "description": "two\nlines"})
	got, _ = os.ReadFile(crlf)
	want := "title = X\r\nname = y\r\ndescription = \"\"\"\r\ntwo\r\nlines\r\n\"\"\"\r\nrelease = 7\r\n"
	if string(got) != want {
		t.Errorf("crlf: %q", got)
	}
	if ReadConf(crlf)["description"] != "two\nlines" {
		t.Errorf("multi-line roundtrip: %q", ReadConf(crlf)["description"])
	}
}

func TestReadWorldMT(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "world.mt"), []byte("gameid = mineclonia\nload_mod_ethereal = true\nload_mod_tnt = false\nload_mod_part = mods/pack/part\n"), 0o644)
	w := ReadWorldMT(dir)
	if w.GameID != "mineclonia" || len(w.Mods) != 3 {
		t.Fatalf("%+v", w)
	}
	if !w.Enabled("ethereal") || w.Enabled("tnt") || !w.Enabled("part") || w.Enabled("nope") {
		t.Errorf("enabled: %+v", w.Mods)
	}
	if w.Mods["part"] != "mods/pack/part" {
		t.Errorf("path value: %q", w.Mods["part"])
	}
}

func TestFolderTypeAndConfFile(t *testing.T) {
	for _, tc := range []struct{ dir, typ string }{
		{"tiny_game", "game"}, {"my_modpack", "modpack"}, {"legacy_mod", "mod"},
	} {
		if got := FolderType(filepath.Join(fixtures, tc.dir)); got != tc.typ {
			t.Errorf("%s: %q", tc.dir, got)
		}
	}
	if ConfFile("txp") != "texture_pack.conf" || ConfFile("mod") != "mod.conf" || ConfFile("modpack") != "modpack.conf" || ConfFile("game") != "game.conf" {
		t.Error("ConfFile")
	}
}
