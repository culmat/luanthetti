package store

import (
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SetMeta("x", "1")
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Meta("x") != "1" || s.Meta("missing") != "" {
		t.Error("meta")
	}
}

func TestPackagesVersionsLinksPins(t *testing.T) {
	s := open(t)
	p, err := s.UpsertPackage(Package{Author: "TenPlus1", Name: "ethereal", Type: "mod", Title: "Ethereal", FolderType: "mod", Folder: "ethereal", Source: "contentdb"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "tenplus1/ethereal" || p.ID == 0 {
		t.Fatalf("%+v", p)
	}
	p2, _ := s.UpsertPackage(Package{Author: "tenplus1", Name: "ethereal", Type: "mod", Title: "Ethereal NG", FolderType: "mod", Folder: "ethereal", Source: "contentdb"})
	if p2.ID != p.ID || p2.Title != "Ethereal NG" {
		t.Errorf("upsert by key: %+v", p2)
	}
	if _, err := s.PackageByKey("nope/nope"); err != ErrNotFound {
		t.Errorf("err = %v", err)
	}
	v1, err := s.AddVersion(Version{PackageID: p.ID, ReleaseID: 100, Path: "/lib/a", Origin: "download"})
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := s.AddVersion(Version{PackageID: p.ID, ReleaseID: 200, Path: "/lib/b", Origin: "download", Complete: true})
	vs, _ := s.VersionsOf(p.ID)
	if len(vs) != 2 || vs[0].ReleaseID != 200 || vs[1].Complete {
		t.Errorf("%+v", vs)
	}
	s.MarkComplete(v1.ID, true)
	if v, _ := s.VersionByID(v1.ID); !v.Complete || v.AddedAt.IsZero() {
		t.Errorf("%+v", v)
	}

	inst, err := s.AddInstallation(Installation{Name: "desktop", Root: "/home/u/.minetest", Kind: "user", EngineVersion: "5.17.0", Protocol: 48})
	if err != nil {
		t.Fatal(err)
	}
	if inst.LinkMode != "auto" || len(inst.ExtraGameDirs) != 0 {
		t.Errorf("%+v", inst)
	}
	if _, err := s.AddInstallation(Installation{Name: "desktop", Root: "/elsewhere", Kind: "user"}); err == nil {
		t.Error("duplicate name accepted")
	}
	byRoot, _ := s.Installation("/home/u/.minetest")
	if byRoot.ID != inst.ID {
		t.Error("lookup by root")
	}
	inst.ExtraGameDirs = []string{"/usr/share/minetest/games"}
	inst.LinkMode = "copy"
	if err := s.UpdateInstallation(inst); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.InstallationByID(inst.ID); got.LinkMode != "copy" || got.ExtraGameDirs[0] != "/usr/share/minetest/games" {
		t.Errorf("%+v", got)
	}

	l, err := s.AddLink(Link{InstallationID: inst.ID, PackageID: p.ID, VersionID: v1.ID, Path: "/home/u/.minetest/mods/ethereal", Kind: "symlink", Reason: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	if l.CreatedAt.IsZero() || len(l.RequiredBy) != 0 {
		t.Errorf("%+v", l)
	}
	l.VersionID = v2.ID
	l.RequiredBy = []string{"x/y"}
	s.UpdateLink(l)
	if got, _ := s.LinkAt(inst.ID, l.Path); got.VersionID != v2.ID || got.RequiredBy[0] != "x/y" {
		t.Errorf("%+v", got)
	}
	if n, _ := s.LinkCount(v2.ID); n != 1 {
		t.Error("link count")
	}
	if links, _ := s.LinksOf(p.ID); len(links) != 1 {
		t.Error("links of")
	}
	rel := int64(100)
	s.SetPin(Pin{InstallationID: inst.ID, PackageID: p.ID, ReleaseID: &rel})
	if pin, _ := s.Pin(inst.ID, p.ID); pin == nil || *pin.ReleaseID != 100 {
		t.Errorf("%+v", pin)
	}
	s.SetPin(Pin{InstallationID: inst.ID, PackageID: p.ID})
	if pin, _ := s.Pin(inst.ID, p.ID); pin == nil || pin.ReleaseID != nil {
		t.Errorf("%+v", pin)
	}
	s.Unpin(inst.ID, p.ID)
	if pin, _ := s.Pin(inst.ID, p.ID); pin != nil {
		t.Error("unpin")
	}
	s.ReplaceScanResults(inst.ID, []ScanResult{{Path: "/x", Status: "adoptable", Author: "a", Name: "b", ReleaseID: 3}})
	if rs, _ := s.ScanResults(inst.ID); len(rs) != 1 || rs[0].Status != "adoptable" || rs[0].ScannedAt.IsZero() {
		t.Errorf("%+v", rs)
	}
	if got, _ := s.InstallationByID(inst.ID); got.LastScanAt.IsZero() {
		t.Error("last_scan_at")
	}
	// Deleting the installation cascades to links, pins and scan results.
	if err := s.DeleteInstallation(inst.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := s.AllLinks(); len(links) != 0 {
		t.Error("cascade")
	}
	if err := s.PutCachedPackage(CachedPackage{Key: p.Key, ReleaseID: 200, DetailJSON: []byte(`{"a":1}`)}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.CachedPackage(p.Key); c.ReleaseID != 200 || string(c.DetailJSON) != `{"a":1}` || c.FetchedAt.IsZero() {
		t.Errorf("%+v", c)
	}
	s.PutCachedBlob("updates:5.17.0:48", []byte(`{}`))
	if b, at, err := s.CachedBlob("updates:5.17.0:48"); err != nil || string(b) != "{}" || at.IsZero() {
		t.Error("blob")
	}
	s.DeleteCachedBlobs("updates:")
	if _, _, err := s.CachedBlob("updates:5.17.0:48"); err != ErrNotFound {
		t.Error("delete blobs")
	}
}
