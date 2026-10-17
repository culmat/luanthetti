package linker

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func lib(t *testing.T, name, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "init.lua"), []byte(content), 0o644)
	return dir
}

func modes() []string {
	m := []string{Auto, Copy}
	if runtime.GOOS == "windows" {
		m = append(m, Junction)
	} else {
		m = append(m, Symlink)
	}
	return m
}

func TestLinkReplaceUnlink(t *testing.T) {
	v1, v2 := lib(t, "v1", "one"), lib(t, "v2", "two")
	for _, mode := range modes() {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "mods", "ethereal")
			kind, err := Link(v1, path, mode)
			if err != nil {
				t.Fatal(err)
			}
			if mode != Auto && kind != mode {
				t.Errorf("kind %s", kind)
			}
			if mode == Auto && kind == Junction {
				t.Error("auto mode must never pick a junction")
			}
			if mode == Auto && runtime.GOOS == "windows" && kind != Copy {
				t.Errorf("auto mode on Windows must copy, got %s", kind)
			}
			if b, _ := os.ReadFile(filepath.Join(path, "init.lua")); string(b) != "one" {
				t.Fatalf("content %q", b)
			}
			in, err := Inspect(path)
			if err != nil || !in.Exists || !in.IsDir {
				t.Fatalf("%+v %v", in, err)
			}
			if kind != Copy && (!in.IsLink || in.Target != v1 && evalOr(in.Target) != evalOr(v1)) {
				t.Errorf("inspect %+v", in)
			}
			if _, err := Link(v1, path, mode); err == nil {
				t.Error("linking over an existing path")
			}
			if err := Replace(root, v2, path, kind); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(filepath.Join(path, "init.lua")); string(b) != "two" {
				t.Errorf("after replace %q", b)
			}
			if err := Unlink(root, path, kind); err != nil {
				t.Fatal(err)
			}
			if in, _ := Inspect(path); in.Exists {
				t.Error("still there")
			}
			// The library folders are untouched.
			if b, _ := os.ReadFile(filepath.Join(v2, "init.lua")); string(b) != "two" {
				t.Error("target damaged")
			}
			if err := Unlink(root, path, kind); err != nil {
				t.Errorf("missing path: %v", err)
			}
		})
	}
}

func evalOr(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestUnlinkSafety(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "mods", "replaced")
	os.MkdirAll(real, 0o755)
	// A real directory where a link was recorded (luma or the in-game updater replaced it).
	if err := Unlink(root, real, Symlink); err == nil {
		t.Error("removed a real directory recorded as a link")
	}
	if _, err := os.Stat(real); err != nil {
		t.Error("directory gone")
	}
	outside := lib(t, "outside", "x")
	if err := Unlink(root, outside, Copy); err == nil {
		t.Error("removed outside the installation")
	}
	if err := Unlink(root, real, Env); err != nil {
		t.Error(err)
	}
}

func TestDanglingAndProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	target := lib(t, "gone", "x")
	path := filepath.Join(root, "mods", "gone")
	Link(target, path, Symlink)
	os.RemoveAll(target)
	in, _ := Inspect(path)
	if !in.IsLink || !in.Dangling {
		t.Errorf("%+v", in)
	}
	if !in.PointsInto(filepath.Dir(target)) {
		t.Error("PointsInto")
	}
	if k := Probe(filepath.Join(root, "mods")); k != Symlink {
		t.Errorf("probe %s", k)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "mods"))
	if len(entries) != 1 {
		t.Errorf("probe left files: %v", entries)
	}
}

func TestEnvModeDoesNothing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mods", "x")
	kind, err := Link(lib(t, "x", "x"), path, Env)
	if err != nil || kind != Env {
		t.Fatal(kind, err)
	}
	if in, _ := Inspect(path); in.Exists {
		t.Error("env mode created something")
	}
}
