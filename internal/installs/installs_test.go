package installs

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseVersionOutput(t *testing.T) {
	out := "Luanti 5.17.0 (OSX)\nUsing LuaJIT 2.1\nBUILD_TYPE=Release\nRUN_IN_PLACE=0\nUSE_CURL=1\n"
	if v, rip := ParseVersionOutput(out); v != "5.17.0" || rip {
		t.Errorf("%q %v", v, rip)
	}
	if v, rip := ParseVersionOutput("Minetest 5.8.0 (Linux)\nRUN_IN_PLACE=1\n"); v != "5.8.0" || !rip {
		t.Errorf("%q %v", v, rip)
	}
	if v, _ := ParseVersionOutput("Luanti 5.18.0-dev-abc (Linux)\n"); v != "5.18.0-dev-abc" {
		t.Errorf("%q", v)
	}
	if v, _ := ParseVersionOutput("something else\n"); v != "" {
		t.Errorf("%q", v)
	}
}

func TestRunInPlaceRoot(t *testing.T) {
	root := filepath.Join("/", "src", "luanti")
	for _, p := range []string{filepath.Join(root, "bin", "luanti"), filepath.Join(root, "bin", "Release", "luanti.exe")} {
		if got := RunInPlaceRoot(p); got != root {
			t.Errorf("%s: %s", p, got)
		}
	}
}

func makeInstall(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	game := filepath.Join(root, "games", "minetest_game")
	os.MkdirAll(filepath.Join(game, "mods", "default"), 0o755)
	os.WriteFile(filepath.Join(game, "game.conf"), []byte("title = Minetest Game\nauthor = Luanti\nrelease = 30000\n"), 0o644)
	os.WriteFile(filepath.Join(game, "mods", "default", "init.lua"), nil, 0o644)
	other := filepath.Join(root, "games", "mineclonia")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "game.conf"), []byte("name = Mineclonia\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "mods", "ethereal"), 0o755)
	os.MkdirAll(filepath.Join(root, "mods", ".hidden"), 0o755)
	w := filepath.Join(root, "worlds", "w1")
	os.MkdirAll(w, 0o755)
	os.WriteFile(filepath.Join(w, "world.mt"), []byte("gameid = minetest\nworld_name = My World\nload_mod_ethereal = true\n"), 0o644)
	return root
}

func TestInspectGamesWorlds(t *testing.T) {
	root := makeInstall(t)
	c := Inspect(root)
	if !c.Exists || c.Mods != 1 || len(c.Games) != 2 {
		t.Fatalf("%+v", c)
	}
	mtg := c.Games[1]
	if mtg.ID != "minetest" || mtg.Folder != "minetest_game" || mtg.ContentDBKey() != "luanti/minetest" || mtg.Title != "Minetest Game" {
		t.Errorf("%+v", mtg)
	}
	if c.Games[0].ID != "mineclonia" || c.Games[0].ContentDBKey() != "" || c.Games[0].Title != "Mineclonia" {
		t.Errorf("%+v", c.Games[0])
	}
	ws := Worlds(root)
	if len(ws) != 1 || ws[0].Name != "My World" || !ws[0].MT.Enabled("ethereal") {
		t.Errorf("%+v", ws)
	}
	if Inspect(t.TempDir()).Exists {
		t.Error("empty dir is not an installation")
	}
	if ContentDir(root, "txp") != filepath.Join(root, "textures") || ContentDir(root, "modpack") != filepath.Join(root, "mods") {
		t.Error("ContentDir")
	}
}

func TestDetectWithEnvAndFakeRunInPlaceBinary(t *testing.T) {
	root := makeInstall(t)
	t.Setenv("LUANTI_USER_PATH", root)
	t.Setenv("MINETEST_USER_PATH", "")
	if runtime.GOOS != "windows" {
		// A RUN_IN_PLACE build directory with a fake binary on PATH.
		build := t.TempDir()
		os.MkdirAll(filepath.Join(build, "bin"), 0o755)
		os.MkdirAll(filepath.Join(build, "mods"), 0o755)
		script := "#!/bin/sh\necho 'Luanti 5.18.0-dev (Linux)'\necho 'RUN_IN_PLACE=1'\n"
		os.WriteFile(filepath.Join(build, "bin", "luanti"), []byte(script), 0o755)
		t.Setenv("PATH", filepath.Join(build, "bin"))
		cs := Detect(context.Background(), false)
		var gotUser, gotRIP bool
		for _, c := range cs {
			real, _ := filepath.EvalSymlinks(root)
			realBuild, _ := filepath.EvalSymlinks(build)
			if c.Root == real && c.Kind == "user" {
				gotUser = true
			}
			if c.Root == realBuild && c.Kind == "run_in_place" && c.EngineVersion == "5.18.0-dev" {
				gotRIP = true
			}
		}
		if !gotUser || !gotRIP {
			t.Errorf("user=%v rip=%v %+v", gotUser, gotRIP, cs)
		}
		SortCandidates(cs)
		if cs[0].Kind != "user" {
			t.Errorf("order %+v", cs)
		}
		return
	}
	cs := Detect(context.Background(), false)
	if len(cs) == 0 || cs[0].Kind != "user" {
		t.Errorf("%+v", cs)
	}
}
