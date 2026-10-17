package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/culmat/luanthetti/internal/contentdb"
	"github.com/culmat/luanthetti/internal/contentdb/fake"
	"github.com/culmat/luanthetti/internal/luanticonf"
	"github.com/culmat/luanthetti/internal/store"
)

const pkgs = "../../testdata/packages"

func setup(t *testing.T) (*Library, *fake.Server) {
	t.Helper()
	srv := fake.New()
	t.Cleanup(srv.Close)
	c := contentdb.New(srv.URL, "luanthetti-test/0", 2)
	c.HTTP = srv.Client()
	c.SetLimit(1000)
	c.Backoff = time.Millisecond
	st, err := store.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Library{Root: filepath.Join(t.TempDir(), "library"), Store: st, CDB: c, Log: t.Logf}, srv
}

func addPkg(t *testing.T, srv *fake.Server, author, name, typ, title, dir, top string, release int64) {
	t.Helper()
	z, err := fake.ZipDir(dir, top)
	if err != nil {
		t.Fatal(err)
	}
	p := &fake.Package{}
	p.Author, p.Name, p.Type, p.Title, p.ShortDescription = author, name, typ, title, title+" description"
	srv.Add(p)
	srv.AddRelease(author, name, release, "v"+title, z)
}

func TestEnsureDownloadsOnceAndWritesConf(t *testing.T) {
	lib, srv := setup(t)
	// The zip wraps the mod in a "repo-main" folder, as git exports do.
	addPkg(t, srv, "Someone", "undeclared_mod", "mod", "Undeclared", filepath.Join(pkgs, "undeclared_mod"), "repo-main", 100)
	ctx := context.Background()
	r := Release{Author: "Someone", Name: "undeclared_mod", Type: "mod", Title: "Undeclared", ShortDescription: "desc", ReleaseID: 100}
	pkg, ver, err := lib.Ensure(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Key != "someone/undeclared_mod" || pkg.FolderType != "mod" || pkg.Folder != "undeclared_mod" || !ver.Complete {
		t.Fatalf("%+v %+v", pkg, ver)
	}
	want := filepath.Join(lib.Root, "mod", "Someone", "undeclared_mod", "100", "undeclared_mod")
	if ver.Path != want {
		t.Errorf("path %s", ver.Path)
	}
	conf := luanticonf.ReadConf(filepath.Join(ver.Path, "mod.conf"))
	if conf["author"] != "Someone" || conf["release"] != "100" || conf["name"] != "undeclared_mod" {
		t.Errorf("conf %v", conf)
	}
	if conf["depends"] != "xcompat" {
		t.Errorf("original keys lost: %v", conf)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ver.Path), ".complete")); err != nil {
		t.Error(".complete missing")
	}
	n := len(srv.Requests)
	if _, _, err := lib.Ensure(ctx, r); err != nil {
		t.Fatal(err)
	}
	if len(srv.Requests) != n {
		t.Errorf("second Ensure downloaded again: %v", srv.Requests[n:])
	}
	// A crash before .complete makes Ensure redo the release.
	os.Remove(filepath.Join(filepath.Dir(ver.Path), ".complete"))
	if _, _, err := lib.Ensure(ctx, r); err != nil {
		t.Fatal(err)
	}
	if len(srv.Requests) == n {
		t.Error("incomplete release was not downloaded again")
	}
}

func TestEnsureModpackGameAndTypeMismatch(t *testing.T) {
	lib, srv := setup(t)
	ctx := context.Background()
	addPkg(t, srv, "mt-mods", "my_modpack", "mod", "Pack", filepath.Join(pkgs, "my_modpack"), "my_modpack", 7)
	_, ver, err := lib.Ensure(ctx, Release{Author: "mt-mods", Name: "my_modpack", Type: "mod", Title: "Pack", ReleaseID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if conf := luanticonf.ReadConf(filepath.Join(ver.Path, "modpack.conf")); conf["release"] != "7" || conf["author"] != "mt-mods" {
		t.Errorf("modpack conf %v", conf)
	}
	addPkg(t, srv, "Tiny", "tiny_game", "game", "Tiny", filepath.Join(pkgs, "tiny_game"), "tiny_game", 9)
	pkg, ver, err := lib.Ensure(ctx, Release{Author: "Tiny", Name: "tiny_game", Type: "game", Title: "Tiny Game", ReleaseID: 9})
	if err != nil {
		t.Fatal(err)
	}
	conf := luanticonf.ReadConf(filepath.Join(ver.Path, "game.conf"))
	if pkg.FolderType != "game" || conf["release"] != "9" || conf["name"] != "" {
		t.Errorf("game: %+v %v (games get no name key)", pkg, conf)
	}
	// ContentDB says txp, but the zip holds a mod: refused, like the engine.
	addPkg(t, srv, "x", "wrongtype", "txp", "Wrong", filepath.Join(pkgs, "legacy_mod"), "wrongtype", 3)
	_, _, err = lib.Ensure(ctx, Release{Author: "x", Name: "wrongtype", Type: "txp", ReleaseID: 3})
	if err == nil || !strings.Contains(err.Error(), "contains a mod, not a txp") {
		t.Errorf("mismatch: %v", err)
	}
	if _, err := os.Stat(lib.ReleaseDir("txp", "x", "wrongtype", 3)); !os.IsNotExist(err) {
		t.Error("failed release left files behind")
	}
}

func TestExtractGuards(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]map[string]string{
		"dotdot":    {"mod/../../evil.lua": "x"},
		"absolute":  {"/etc/evil": "x"},
		"backslash": {`mod\..\evil`: "x"},
	}
	for name, files := range cases {
		zp := filepath.Join(dir, name+".zip")
		os.WriteFile(zp, fake.ZipFiles(files), 0o644)
		if err := Extract(zp, filepath.Join(dir, name), 1<<20); !errors.Is(err, ErrUnsafeZip) {
			t.Errorf("%s: %v", name, err)
		}
	}
	zp := filepath.Join(dir, "big.zip")
	os.WriteFile(zp, fake.ZipFiles(map[string]string{"a": strings.Repeat("x", 2000)}), 0o644)
	if err := Extract(zp, filepath.Join(dir, "big"), 1000); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("size: %v", err)
	}
	if err := Extract(filepath.Join(dir, "missing.zip"), filepath.Join(dir, "m"), 10); err == nil {
		t.Error("missing zip")
	}
}

func TestPruneKeepsLinkedAndPinned(t *testing.T) {
	lib, srv := setup(t)
	ctx := context.Background()
	addPkg(t, srv, "a", "legacy_mod", "mod", "L", filepath.Join(pkgs, "legacy_mod"), "legacy_mod", 1)
	z, _ := fake.ZipDir(filepath.Join(pkgs, "legacy_mod"), "legacy_mod")
	for _, id := range []int64{2, 3, 4, 5} {
		srv.AddRelease("a", "legacy_mod", id, "r", z)
	}
	var vers []store.Version
	var pkg store.Package
	for _, id := range []int64{1, 2, 3, 4, 5} {
		p, v, err := lib.Ensure(ctx, Release{Author: "a", Name: "legacy_mod", Type: "mod", ReleaseID: id})
		if err != nil {
			t.Fatal(err)
		}
		pkg, vers = p, append(vers, v)
	}
	inst, _ := lib.Store.AddInstallation(store.Installation{Name: "i", Root: "/i", Kind: "user"})
	lib.Store.AddLink(store.Link{InstallationID: inst.ID, PackageID: pkg.ID, VersionID: vers[0].ID, Path: "/i/mods/legacy_mod", Kind: "symlink", Reason: "explicit"})
	rel := int64(2)
	lib.Store.SetPin(store.Pin{InstallationID: inst.ID, PackageID: pkg.ID, ReleaseID: &rel})
	removed, err := lib.Prune(pkg.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 5 is the newest unreferenced (kept), 1 is linked, 2 is pinned; 3 and 4 go.
	if len(removed) != 2 || removed[0].ReleaseID != 4 || removed[1].ReleaseID != 3 {
		t.Errorf("removed %+v", removed)
	}
	left, _ := lib.Store.VersionsOf(pkg.ID)
	if len(left) != 3 {
		t.Errorf("left %+v", left)
	}
	if _, err := os.Stat(filepath.Dir(vers[2].Path)); !os.IsNotExist(err) {
		t.Error("files of release 3 remain")
	}
	if err := lib.RemoveVersion(vers[0]); err == nil {
		t.Error("removed a linked version")
	}
}

func TestAddLocalAndAdopt(t *testing.T) {
	lib, _ := setup(t)
	pkg, ver, err := lib.AddLocal(filepath.Join(pkgs, "my_modpack"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Source != "local" || pkg.Key != "local/my_modpack" || pkg.FolderType != "modpack" || ver.ReleaseID != 0 || !ver.Complete {
		t.Errorf("%+v %+v", pkg, ver)
	}
	// Adopt moves a directory that already has author/release in its conf.
	src := filepath.Join(t.TempDir(), "mods", "legacy_mod")
	os.MkdirAll(filepath.Dir(src), 0o755)
	if err := copyTree(filepath.Join(pkgs, "legacy_mod"), src); err != nil {
		t.Fatal(err)
	}
	r := Release{Author: "TenPlus1", Name: "legacy_mod", Type: "mod", ReleaseID: 42}
	p2, v2, moved, err := lib.Adopt(src, r, "mod")
	if err != nil || !moved || p2.Key != "tenplus1/legacy_mod" || v2.Origin != "adopted" {
		t.Fatalf("%v %v %+v %+v", err, moved, p2, v2)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source not moved")
	}
	// Adopting the same release again keeps the source and reuses the version.
	os.MkdirAll(src, 0o755)
	_, v3, moved, err := lib.Adopt(src, r, "mod")
	if err != nil || moved || v3.ID != v2.ID {
		t.Errorf("%v %v %+v", err, moved, v3)
	}
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
}

func TestCheckType(t *testing.T) {
	for _, c := range []struct {
		typ, dir, want string
		ok             bool
	}{
		{"mod", "legacy_mod", "mod", true},
		{"mod", "my_modpack", "modpack", true},
		{"mod", "tiny_game", "", false},
		{"game", "tiny_game", "game", true},
		{"game", "legacy_mod", "", false},
		{"txp", "legacy_mod", "", false},
	} {
		got, err := CheckType(c.typ, filepath.Join(pkgs, c.dir))
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("%s %s: %q %v", c.typ, c.dir, got, err)
		}
	}
	empty := t.TempDir()
	if got, err := CheckType("txp", empty); err != nil || got != "txp" {
		t.Errorf("txp without conf: %q %v", got, err)
	}
}
