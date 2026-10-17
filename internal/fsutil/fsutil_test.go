package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWithin(t *testing.T) {
	root := filepath.Join("a", "b")
	for path, want := range map[string]bool{
		filepath.Join("a", "b"):            true,
		filepath.Join("a", "b", "c"):       true,
		filepath.Join("a", "bc"):           false,
		filepath.Join("a", "b", "..", "x"): false,
		filepath.Join("a"):                 false,
	} {
		if Within(root, path) != want {
			t.Errorf("Within(%s, %s) != %v", root, path, want)
		}
	}
}

func TestCopyDirSkipsGitAndOutsideLinks(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, ".git"), 0o755)
	os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(src, "textures"), 0o755)
	os.WriteFile(filepath.Join(src, "init.lua"), []byte("-- hi"), 0o644)
	os.WriteFile(filepath.Join(src, "textures", "a.png"), []byte("png"), 0o644)
	if runtime.GOOS != "windows" {
		os.Symlink("init.lua", filepath.Join(src, "inside"))
		os.Symlink("/etc/passwd", filepath.Join(src, "outside"))
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Error(".git copied")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "textures", "a.png")); string(b) != "png" {
		t.Error("file content")
	}
	if runtime.GOOS != "windows" {
		if l, err := os.Readlink(filepath.Join(dst, "inside")); err != nil || l != "init.lua" {
			t.Errorf("inside link: %q %v", l, err)
		}
		if _, err := os.Lstat(filepath.Join(dst, "outside")); !os.IsNotExist(err) {
			t.Error("link escaping the tree was copied")
		}
	}
	if err := RemoveAllWithin(filepath.Dir(dst), dst); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllWithin(dst, filepath.Dir(dst)); err == nil {
		t.Error("removed outside root")
	}
	moved := filepath.Join(t.TempDir(), "x", "moved")
	if err := MoveDir(src, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, "init.lua")); err != nil {
		t.Error("move")
	}
}
