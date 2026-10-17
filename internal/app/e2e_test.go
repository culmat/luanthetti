package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/culmat/luanthetti/internal/contentdb"
	"github.com/culmat/luanthetti/internal/contentdb/fake"
	"github.com/culmat/luanthetti/internal/linker"
	"github.com/culmat/luanthetti/internal/luanticonf"
)

const miniSnapshot = "../depgraph/testdata/mini.sqlite"

// modZip builds a release zip holding one mod folder.
func modZip(t *testing.T, name, extra string) []byte {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "init.lua"), []byte("-- "+name+" "+extra), 0o644)
	os.WriteFile(filepath.Join(dir, "mod.conf"), []byte("name = "+name+"\n# keep me\n"), 0o644)
	z, err := fake.ZipDir(dir, name+"-main")
	if err != nil {
		t.Fatal(err)
	}
	return z
}

type env struct {
	app  *App
	srv  *fake.Server
	inst string // root of the first installation
}

func newEnv(t *testing.T) *env {
	t.Helper()
	srv := fake.New()
	t.Cleanup(srv.Close)
	add := func(author, name, typ, title string, release int64) {
		p := &fake.Package{}
		p.Author, p.Name, p.Type, p.Title, p.ShortDescription = author, name, typ, title, title+"!"
		srv.Add(p)
		srv.AddRelease(author, name, release, "v1", modZip(t, name, "v1"))
	}
	add("mt-mods", "pipeworks", "mod", "Pipeworks", 201)
	add("other", "xcompat", "mod", "xcompat", 300)
	add("mt-mods", "xcompat", "mod", "xcompat", 200)
	add("TenPlus1", "ethereal", "mod", "Ethereal", 202)
	srv.Tags = []contentdb.Tag{{Name: "tech", Title: "Technology"}}

	a, err := New(Options{Home: t.TempDir(), ContentDBURL: srv.URL, SnapshotFile: miniSnapshot,
		HTTPClient: srv.Client(), ContentDBRPS: 1000, Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return &env{app: a, srv: srv, inst: makeInstallation(t, "Luanti", "minetest_game")}
}

// makeInstallation creates a user path with one game.
func makeInstallation(t *testing.T, gameAuthor, gameFolder string) string {
	t.Helper()
	root := t.TempDir()
	g := filepath.Join(root, "games", gameFolder)
	os.MkdirAll(filepath.Join(g, "mods", "default"), 0o755)
	os.WriteFile(filepath.Join(g, "game.conf"), []byte("title = Game\nauthor = "+gameAuthor+"\nrelease = 100\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "mods"), 0o755)
	w := filepath.Join(root, "worlds", "w")
	os.MkdirAll(w, 0o755)
	os.WriteFile(filepath.Join(w, "world.mt"), []byte("gameid = minetest\nload_mod_pipeworks = true\n"), 0o644)
	return root
}

func TestEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based end-to-end flow; the linker's junction and copy paths are tested in internal/linker")
	}
	e := newEnv(t)
	ctx := context.Background()
	a := e.app

	inst, err := a.AddInstall(ctx, AddInstallRequest{Path: e.inst, Name: "desktop", EngineVersion: "5.17.0"})
	if err != nil {
		t.Fatal(err)
	}
	if inst.LinkMode != linker.Symlink {
		t.Errorf("link mode %s", inst.LinkMode)
	}
	views, _ := a.ListInstalls(ctx)
	if len(views) != 1 || len(views[0].Games) != 1 || views[0].Games[0].Key != "Luanti/minetest_game" {
		t.Fatalf("%+v", views)
	}

	// Install: pipeworks needs xcompat; in Minetest Game the resolver picks other/xcompat.
	confirmed := false
	rep, err := a.Install(ctx, InstallRequest{Specs: []string{"mt-mods/pipeworks"}, Confirm: func(*InstallReport) (bool, error) {
		confirmed = true
		return true, nil
	}})
	if err != nil {
		t.Fatalf("%v %+v", err, rep)
	}
	if !confirmed {
		t.Error("new dependency was not confirmed")
	}
	items := rep.Results[0].Items
	if len(items) != 2 || items[0].Package != "mt-mods/pipeworks" || items[1].Package != "other/xcompat" || items[1].Reason != "dependency" {
		t.Fatalf("%+v", items)
	}
	if rep.Results[0].Games[0].Status != "risky" { // mini-patch.sql: undeclared mcl_core use
		t.Errorf("game status %+v", rep.Results[0].Games)
	}
	pw := filepath.Join(e.inst, "mods", "pipeworks")
	in, _ := linker.Inspect(pw)
	if !in.IsLink {
		t.Fatalf("not linked: %+v", in)
	}
	conf := luanticonf.ReadConf(filepath.Join(pw, "mod.conf"))
	if conf["author"] != "mt-mods" || conf["release"] != "201" || conf["name"] != "pipeworks" || conf["title"] != "Pipeworks" {
		t.Errorf("conf %v", conf)
	}
	list, _ := a.List(ctx, ListRequest{})
	if len(list.Rows) != 2 || list.Rows[0].State != "ok" || list.Rows[1].State != "ok" {
		t.Errorf("%+v", list.Rows)
	}

	// Installing again is a no-op.
	rep, err = a.Install(ctx, InstallRequest{Specs: []string{"pipeworks"}})
	if err != nil || rep.Results[0].Items[0].Status != "present" {
		t.Errorf("%v %+v", err, rep)
	}

	// Update: a new release appears.
	e.srv.AddRelease("mt-mods", "pipeworks", 301, "v2", modZip(t, "pipeworks", "v2"))
	a.Store.DeleteCachedBlobs("updates:")
	up, err := a.Update(ctx, UpdateRequest{Check: true})
	if err != nil || len(up.Items) != 1 || up.Items[0].Status != "outdated" || up.Items[0].Latest != 301 {
		t.Fatalf("%v %+v", err, up)
	}
	up, err = a.Update(ctx, UpdateRequest{})
	if err != nil || up.Items[0].Status != "updated" {
		t.Fatalf("%v %+v", err, up)
	}
	if conf := luanticonf.ReadConf(filepath.Join(pw, "mod.conf")); conf["release"] != "301" {
		t.Errorf("after update %v", conf)
	}
	b, _ := os.ReadFile(filepath.Join(pw, "init.lua"))
	if !strings.Contains(string(b), "v2") {
		t.Errorf("content %q", b)
	}
	lib, _ := a.List(ctx, ListRequest{Library: true})
	for _, r := range lib.Library {
		if r.Package == "mt-mods/pipeworks" && len(r.Versions) != 2 {
			t.Errorf("old version should be kept: %+v", r.Versions)
		}
	}

	// Rollback pins the old release; update then reports it pinned.
	rb, err := a.Rollback(ctx, "pipeworks", nil, 0)
	if err != nil || rb[0].Status != "updated" || rb[0].Latest != 201 {
		t.Fatalf("%v %+v", err, rb)
	}
	up, _ = a.Update(ctx, UpdateRequest{})
	if len(up.Items) != 1 || up.Items[0].Status != "pinned" {
		t.Errorf("%+v", up.Items)
	}
	if _, err := a.Pin(ctx, "pipeworks", nil, 0, true); err != nil {
		t.Fatal(err)
	}

	// A second installation: install there, then remove from the first; the library stays.
	second := makeInstallation(t, "Luanti", "minetest_game")
	if _, err := a.AddInstall(ctx, AddInstallRequest{Path: second, Name: "server", EngineVersion: "5.17.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Install(ctx, InstallRequest{Specs: []string{"mt-mods/pipeworks"}, Into: []string{"server"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Remove(ctx, RemoveRequest{Specs: []string{"other/xcompat"}, Into: []string{"desktop"}}); err == nil {
		t.Error("removed a dependency that pipeworks needs")
	}
	rr, err := a.Remove(ctx, RemoveRequest{Specs: []string{"pipeworks"}, Into: []string{"desktop"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Removed) != 2 || len(rr.Library) != 0 {
		t.Errorf("removed %+v library %v", rr.Removed, rr.Library)
	}
	if !strings.Contains(strings.Join(rr.Warnings, " "), "load_mod_pipeworks") {
		t.Errorf("world warning missing: %v", rr.Warnings)
	}
	if in, _ := linker.Inspect(pw); in.Exists {
		t.Error("link still there")
	}

	// Scan and adopt: a folder installed by the in-game client.
	eth := filepath.Join(e.inst, "mods", "ethereal")
	os.MkdirAll(eth, 0o755)
	os.WriteFile(filepath.Join(eth, "init.lua"), nil, 0o644)
	os.WriteFile(filepath.Join(eth, "mod.conf"), []byte("name = ethereal\nauthor = TenPlus1\nrelease = 202\n"), 0o644)
	git := filepath.Join(e.inst, "mods", "mydev")
	os.MkdirAll(filepath.Join(git, ".git"), 0o755)
	os.WriteFile(filepath.Join(git, "init.lua"), nil, 0o644)
	sc, err := a.Scan(ctx, "desktop")
	// ethereal and the game (its game.conf has author and release) are adoptable.
	if err != nil || sc.Counts["adoptable"] != 2 || sc.Counts["git"] != 1 {
		t.Fatalf("%v %+v", err, sc)
	}
	ad, err := a.Adopt(ctx, AdoptRequest{Installation: "desktop", Specs: []string{"ethereal"}})
	if err != nil || len(ad.Adopted) != 1 || ad.Adopted[0].Action != "moved" || ad.Adopted[0].Package != "TenPlus1/ethereal" {
		t.Fatalf("%v %+v", err, ad)
	}
	if in, _ := linker.Inspect(eth); !in.IsLink {
		t.Error("adopted folder not replaced by a link")
	}

	// Another tool replaces the link with a real folder: verify notices, repair re-adopts.
	os.Remove(eth)
	os.MkdirAll(eth, 0o755)
	os.WriteFile(filepath.Join(eth, "init.lua"), []byte("newer"), 0o644)
	os.WriteFile(filepath.Join(eth, "mod.conf"), []byte("name = ethereal\nauthor = TenPlus1\nrelease = 250\n"), 0o644)
	vr, err := a.Verify(ctx, []string{"desktop"})
	if err != nil || len(vr.Problems) != 1 || vr.Problems[0].Kind != "replaced" {
		t.Fatalf("%v %+v", err, vr)
	}
	rp, err := a.Repair(ctx, RepairRequest{Into: []string{"desktop"}, Readopt: true})
	if err != nil || len(rp.Repaired) != 1 || len(rp.Problems) != 0 {
		t.Fatalf("%v %+v", err, rp)
	}
	list, _ = a.List(ctx, ListRequest{Into: []string{"desktop"}})
	if len(list.Rows) != 1 || list.Rows[0].Release != 250 || list.Rows[0].State != "ok" {
		t.Errorf("%+v", list.Rows)
	}

	// A broken link: repair --relink restores it.
	os.Remove(eth)
	vr, _ = a.Verify(ctx, []string{"desktop"})
	if len(vr.Problems) != 1 || vr.Problems[0].Kind != "missing" {
		t.Fatalf("%+v", vr.Problems)
	}
	if rp, _ := a.Repair(ctx, RepairRequest{Into: []string{"desktop"}, Relink: true}); len(rp.Repaired) != 1 {
		t.Errorf("%+v", rp)
	}

	// Search with filters and compat marks for the installations' games.
	sr, err := a.Search(ctx, SearchQuery{Q: "xcompat", Type: "mod"})
	if err != nil || sr.Total != 2 || sr.Hits[0].Package != "other/xcompat" || sr.Hits[0].Compat["Luanti/minetest_game"] != "ok" {
		t.Fatalf("%v %+v", err, sr)
	}
	sr, _ = a.Search(ctx, SearchQuery{DevStates: []string{"actively-developed"}})
	if sr.Total != 1 || sr.Hits[0].Package != "mt-mods/pipeworks" || len(sr.Hits[0].InstalledIn) != 1 {
		t.Errorf("%+v", sr.Hits)
	}
	sr, _ = a.Search(ctx, SearchQuery{CompatibleWith: []string{"mineclonia"}, Type: "mod"})
	for _, h := range sr.Hits {
		if h.Package == "TenPlus1/ethereal" {
			t.Error("ethereal can't run in Mineclonia")
		}
	}

	// Info merges snapshot, ContentDB and compat.
	info, err := a.Info(ctx, "TenPlus1/ethereal")
	if err != nil || info.Source != "live" || len(info.Releases) != 1 || len(info.Compat) < 3 || len(info.Installed) != 1 {
		t.Fatalf("%v %+v", err, info)
	}
	if info.Compat[0].Game != "Luanti/minetest_game" || info.Compat[0].Status != "ok" {
		t.Errorf("%+v", info.Compat[0])
	}

	envr, _ := a.Env(ctx, "server")
	if len(envr.WorldLines) != 2 || !strings.HasPrefix(envr.Env, "LUANTI_MOD_PATH=") {
		t.Errorf("%+v", envr)
	}
	if d := a.Doctor(ctx); d.Snapshot == nil || !d.Precomputed || len(d.Installations) != 2 {
		t.Errorf("%+v", d)
	}
}

func TestInstallRefusesWhereNoGameWorksAndUnmanagedConflicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based flow")
	}
	e := newEnv(t)
	ctx := context.Background()
	mcl := makeInstallation(t, "ryvnf", "mineclonia")
	if _, err := e.app.AddInstall(ctx, AddInstallRequest{Path: mcl, Name: "mcl", EngineVersion: "5.17.0"}); err != nil {
		t.Fatal(err)
	}
	rep, err := e.app.Install(ctx, InstallRequest{Specs: []string{"TenPlus1/ethereal"}})
	if err != nil || rep.Results[0].Skipped == "" || len(rep.Results[0].Items) != 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	// An unmanaged folder with the same name blocks the install.
	os.MkdirAll(filepath.Join(mcl, "mods", "pipeworks"), 0o755)
	os.WriteFile(filepath.Join(mcl, "mods", "pipeworks", "init.lua"), nil, 0o644)
	rep, err = e.app.Install(ctx, InstallRequest{Specs: []string{"mt-mods/pipeworks"}})
	if err != nil {
		t.Fatal(err)
	}
	if it := rep.Results[0].Items[0]; it.Status != "skipped" || !strings.Contains(it.Note, "not managed") {
		t.Errorf("%+v", it)
	}
	if it := rep.Results[0].Items[1]; it.Status != "skipped" || !strings.Contains(it.Note, "not needed") {
		t.Errorf("dependency of a skipped package was installed: %+v", it)
	}
	// Dry run changes nothing.
	os.RemoveAll(filepath.Join(mcl, "mods", "pipeworks"))
	rep, _ = e.app.Install(ctx, InstallRequest{Specs: []string{"mt-mods/pipeworks"}, DryRun: true})
	if !rep.DryRun || rep.Results[0].Items[0].Status != "planned" || rep.Results[0].Items[1].Package != "mt-mods/xcompat" {
		t.Errorf("%+v", rep.Results[0].Items)
	}
	if in, _ := linker.Inspect(filepath.Join(mcl, "mods", "pipeworks")); in.Exists {
		t.Error("dry run linked")
	}
	if _, err := e.app.Install(ctx, InstallRequest{Specs: []string{"mt-mods/pipeworks"}, Confirm: func(*InstallReport) (bool, error) { return false, nil }}); err != ErrDeclined {
		t.Errorf("declined: %v", err)
	}
	// A local package.
	local := filepath.Join(t.TempDir(), "mytool")
	os.MkdirAll(local, 0o755)
	os.WriteFile(filepath.Join(local, "init.lua"), nil, 0o644)
	rep, err = e.app.Install(ctx, InstallRequest{Specs: []string{local}})
	if err != nil || rep.Results[0].Items[0].Status != "installed" || rep.Results[0].Items[0].Package != "local/mytool" {
		t.Fatalf("%v %+v", err, rep)
	}
}
