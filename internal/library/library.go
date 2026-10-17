// Package library stores each package release once, as a Luanti-ready folder:
//
//	<root>/<mod|game|txp>/<author>/<name>/<release_id>/<folder>/
//	                                                   release.json
//	                                                   .complete      (download URL, written last)
//
// Downloads are extracted with a zip-slip guard, unwrapped to the content root like the
// engine does, type-checked, and get the same author/name/release keys in their .conf as the
// in-game installer writes (builtin/mainmenu/content/contentdb.lua), so Luanti, luma and
// Luanthetti recognise them.
package library

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/culmat/luanthetti/internal/contentdb"
	"github.com/culmat/luanthetti/internal/fsutil"
	"github.com/culmat/luanthetti/internal/luanticonf"
	"github.com/culmat/luanthetti/internal/store"
)

// DefaultMaxSize caps the uncompressed size of one release.
const DefaultMaxSize = 2 << 30

// Library is the package store on disk plus its records.
type Library struct {
	Root    string
	Store   *store.Store
	CDB     *contentdb.Client
	MaxSize int64
	Log     func(format string, args ...any)
}

// Release identifies what to put into the library.
type Release struct {
	Author           string `json:"author"`
	Name             string `json:"name"`
	Type             string `json:"type"` // mod | game | txp
	Title            string `json:"title"`
	ShortDescription string `json:"short_description"`
	ReleaseID        int64  `json:"release_id"`
	ReleaseTitle     string `json:"release_title,omitempty"`
	ReleaseDate      string `json:"release_date,omitempty"`
	MinEngine        string `json:"min_engine,omitempty"`
	MaxEngine        string `json:"max_engine,omitempty"`
}

// Record is release.json.
type Record struct {
	Release
	FolderType string    `json:"folder_type"`
	Folder     string    `json:"folder"`
	Origin     string    `json:"origin"`
	URL        string    `json:"url,omitempty"`
	AddedAt    time.Time `json:"added_at"`
}

func (l *Library) logf(format string, args ...any) {
	if l.Log != nil {
		l.Log(format, args...)
	}
}

// ReleaseDir is where a release lives.
func (l *Library) ReleaseDir(typ, author, name string, release int64) string {
	return filepath.Join(l.Root, typ, author, name, strconv.FormatInt(release, 10))
}

// Ensure makes the release available in the library, downloading it once.
func (l *Library) Ensure(ctx context.Context, r Release) (store.Package, store.Version, error) {
	if r.Author == "" || r.Name == "" || r.ReleaseID <= 0 {
		return store.Package{}, store.Version{}, fmt.Errorf("library: incomplete release %+v", r)
	}
	if !luanticonf.ModNameRE.MatchString(r.Name) {
		return store.Package{}, store.Version{}, fmt.Errorf("library: %q is not a valid package name", r.Name)
	}
	dir := l.ReleaseDir(r.Type, r.Author, r.Name, r.ReleaseID)
	if pkg, ver, ok := l.existing(r, dir); ok {
		return pkg, ver, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return store.Package{}, store.Version{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return store.Package{}, store.Version{}, err
	}
	url := l.CDB.DownloadURL(r.Author, r.Name, r.ReleaseID)
	l.logf("downloading %s/%s release %d", r.Author, r.Name, r.ReleaseID)
	zipPath := filepath.Join(dir, "download.zip.part")
	if err := l.download(ctx, r, zipPath); err != nil {
		os.RemoveAll(dir)
		return store.Package{}, store.Version{}, err
	}
	pkg, ver, err := l.install(r, dir, zipPath, "download", url)
	if err != nil {
		os.RemoveAll(dir)
		return store.Package{}, store.Version{}, err
	}
	return pkg, ver, nil
}

func (l *Library) existing(r Release, dir string) (store.Package, store.Version, bool) {
	if _, err := os.Stat(filepath.Join(dir, ".complete")); err != nil {
		return store.Package{}, store.Version{}, false
	}
	pkg, err := l.Store.PackageByKey(store.Key(r.Author, r.Name))
	if err != nil {
		return store.Package{}, store.Version{}, false
	}
	ver, err := l.Store.VersionByRelease(pkg.ID, r.ReleaseID)
	if err != nil || !ver.Complete {
		return store.Package{}, store.Version{}, false
	}
	if _, err := os.Stat(ver.Path); err != nil {
		return store.Package{}, store.Version{}, false
	}
	return pkg, ver, true
}

func (l *Library) download(ctx context.Context, r Release, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = l.CDB.Download(ctx, r.Author, r.Name, r.ReleaseID, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// install extracts zipPath (or uses an already extracted tree when zipPath is a directory),
// places the content root as <dir>/<folder>, edits the conf and records the version.
func (l *Library) install(r Release, dir, zipPath, origin, url string) (store.Package, store.Version, error) {
	extract := filepath.Join(dir, "extract")
	if err := Extract(zipPath, extract, l.maxSize()); err != nil {
		return store.Package{}, store.Version{}, err
	}
	os.Remove(zipPath)
	root := luanticonf.FindContentRoot(extract, 2)
	folderType, err := CheckType(r.Type, root)
	if err != nil {
		return store.Package{}, store.Version{}, fmt.Errorf("%s/%s: %w", r.Author, r.Name, err)
	}
	folder := r.Name
	target := filepath.Join(dir, folder)
	if err := os.Rename(root, target); err != nil {
		return store.Package{}, store.Version{}, err
	}
	os.RemoveAll(extract)
	if err := WriteConf(target, folderType, r); err != nil {
		return store.Package{}, store.Version{}, err
	}
	return l.record(r, dir, target, folderType, folder, origin, url)
}

func (l *Library) record(r Release, dir, target, folderType, folder, origin, url string) (store.Package, store.Version, error) {
	rec := Record{Release: r, FolderType: folderType, Folder: folder, Origin: origin, URL: url, AddedAt: time.Now().UTC()}
	data, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "release.json"), data, 0o644); err != nil {
		return store.Package{}, store.Version{}, err
	}
	source := "contentdb"
	if origin == "local" {
		source = "local"
	}
	pkg, err := l.Store.UpsertPackage(store.Package{Author: r.Author, Name: r.Name, Type: r.Type, Title: r.Title,
		ShortDescription: r.ShortDescription, Source: source, FolderType: folderType, Folder: folder})
	if err != nil {
		return store.Package{}, store.Version{}, err
	}
	ver, err := l.Store.AddVersion(store.Version{PackageID: pkg.ID, ReleaseID: r.ReleaseID, Path: target, Title: r.ReleaseTitle,
		MinEngine: r.MinEngine, MaxEngine: r.MaxEngine, ReleaseDate: r.ReleaseDate, Origin: origin, Complete: false})
	if err != nil {
		return store.Package{}, store.Version{}, err
	}
	// .complete is written last: a crash before it leaves a directory that Ensure redoes.
	if err := os.WriteFile(filepath.Join(dir, ".complete"), []byte(url+"\n"), 0o644); err != nil {
		return store.Package{}, store.Version{}, err
	}
	if err := l.Store.MarkComplete(ver.ID, true); err != nil {
		return store.Package{}, store.Version{}, err
	}
	ver.Complete = true
	return pkg, ver, nil
}

func (l *Library) maxSize() int64 {
	if l.MaxSize > 0 {
		return l.MaxSize
	}
	return DefaultMaxSize
}

// Adopt moves an existing package directory (already carrying author and release in its
// conf) into the library as a version, or, if that release is already there, returns the
// existing version and leaves src alone. moved reports whether src was moved.
func (l *Library) Adopt(src string, r Release, folderType string) (pkg store.Package, ver store.Version, moved bool, err error) {
	dir := l.ReleaseDir(r.Type, r.Author, r.Name, r.ReleaseID)
	if p, v, ok := l.existing(r, dir); ok {
		return p, v, false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return store.Package{}, store.Version{}, false, err
	}
	target := filepath.Join(dir, r.Name)
	if err := fsutil.MoveDir(src, target); err != nil {
		return store.Package{}, store.Version{}, false, err
	}
	pkg, ver, err = l.record(r, dir, target, folderType, r.Name, "adopted", "")
	return pkg, ver, true, err
}

// AddLocal copies a local directory or zip into the library as a package with source=local.
// Its identity is local/<name>, release 0, and it is never updated.
func (l *Library) AddLocal(path string) (store.Package, store.Version, error) {
	st, err := os.Stat(path)
	if err != nil {
		return store.Package{}, store.Version{}, err
	}
	tmp, err := os.MkdirTemp(l.Root, ".local-")
	if err != nil {
		if err := os.MkdirAll(l.Root, 0o755); err != nil {
			return store.Package{}, store.Version{}, err
		}
		if tmp, err = os.MkdirTemp(l.Root, ".local-"); err != nil {
			return store.Package{}, store.Version{}, err
		}
	}
	defer os.RemoveAll(tmp)
	// Keep the original folder (or zip) name: a mod without mod.conf is named after its folder.
	base := strings.TrimSuffix(filepath.Base(filepath.Clean(path)), filepath.Ext(path))
	if st.IsDir() {
		base = filepath.Base(filepath.Clean(path))
	}
	src := filepath.Join(tmp, base)
	if st.IsDir() {
		err = fsutil.CopyDir(path, src)
	} else {
		err = Extract(path, src, l.maxSize())
	}
	if err != nil {
		return store.Package{}, store.Version{}, err
	}
	scanned, err := luanticonf.ScanPackage(src)
	if err != nil {
		return store.Package{}, store.Version{}, err
	}
	typ := map[string]string{"mod": "mod", "modpack": "mod", "game": "game", "txp": "txp"}[scanned.Type]
	if typ == "" {
		return store.Package{}, store.Version{}, fmt.Errorf("%s is not a mod, modpack, game or texture pack", path)
	}
	name := scanned.Name
	if !luanticonf.ModNameRE.MatchString(name) {
		return store.Package{}, store.Version{}, fmt.Errorf("%s: %q is not a valid name", path, name)
	}
	r := Release{Author: "local", Name: name, Type: typ, Title: scanned.Title, ShortDescription: scanned.Description}
	dir := filepath.Join(l.Root, "local", name, "0")
	if err := os.RemoveAll(dir); err != nil {
		return store.Package{}, store.Version{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return store.Package{}, store.Version{}, err
	}
	target := filepath.Join(dir, name)
	if err := os.Rename(scanned.Root, target); err != nil {
		return store.Package{}, store.Version{}, err
	}
	return l.record(r, dir, target, scanned.Type, name, "local", "")
}

// Prune deletes unreferenced versions of a package beyond the newest keep ones. A version is
// referenced when a link points at it or a pin names its release.
func (l *Library) Prune(packageID int64, keep int) ([]store.Version, error) {
	versions, err := l.Store.VersionsOf(packageID)
	if err != nil {
		return nil, err
	}
	pinned := map[int64]bool{}
	pins, err := l.Store.Pins()
	if err != nil {
		return nil, err
	}
	for _, p := range pins {
		if p.PackageID == packageID && p.ReleaseID != nil {
			pinned[*p.ReleaseID] = true
		}
	}
	var removed []store.Version
	kept := 0
	for _, v := range versions {
		n, err := l.Store.LinkCount(v.ID)
		if err != nil {
			return removed, err
		}
		if n > 0 || pinned[v.ReleaseID] {
			continue
		}
		if kept < keep && v.Complete {
			kept++
			continue
		}
		if err := l.RemoveVersion(v); err != nil {
			return removed, err
		}
		removed = append(removed, v)
	}
	return removed, nil
}

// RemoveVersion deletes a version's files and row. It refuses versions that are still linked.
func (l *Library) RemoveVersion(v store.Version) error {
	if n, err := l.Store.LinkCount(v.ID); err != nil {
		return err
	} else if n > 0 {
		return fmt.Errorf("release %d is still linked in %d place(s)", v.ReleaseID, n)
	}
	if err := fsutil.RemoveAllWithin(l.Root, filepath.Dir(v.Path)); err != nil {
		return err
	}
	return l.Store.DeleteVersion(v.ID)
}

// CheckType verifies the extracted content against the ContentDB type, the way the engine's
// pkgmgr.install_dir does, and returns the folder type (mod, modpack, game, txp).
func CheckType(cdbType, root string) (string, error) {
	got := luanticonf.FolderType(root)
	switch cdbType {
	case "mod":
		if got == "mod" || got == "modpack" {
			return got, nil
		}
		// A repo of several mods without modpack.conf: the engine installs it as a modpack.
		if got == "" && len(luanticonf.CollectMods(root, root)) > 0 {
			return "modpack", nil
		}
	case "game":
		if got == "game" {
			return got, nil
		}
	case "txp":
		// There's no reliable way to detect a texture pack; refuse only the clear mismatches.
		if got == "" || got == "txp" {
			return "txp", nil
		}
	default:
		return "", fmt.Errorf("unknown package type %q", cdbType)
	}
	if got == "" {
		got = "nothing recognisable"
	}
	return "", fmt.Errorf("the release contains a %s, not a %s", got, cdbType)
}

// WriteConf sets the keys the in-game installer sets after extracting (contentdb.lua):
// title and description only when absent, name except for games, author and release always.
func WriteConf(dir, folderType string, r Release) error {
	path := filepath.Join(dir, luanticonf.ConfFile(folderType))
	conf := luanticonf.ReadConf(path)
	set := map[string]string{"author": r.Author, "release": strconv.FormatInt(r.ReleaseID, 10)}
	if conf["title"] == "" && r.Title != "" {
		set["title"] = r.Title
	}
	if folderType != "game" {
		set["name"] = r.Name
	}
	if conf["description"] == "" && r.ShortDescription != "" {
		set["description"] = r.ShortDescription
	}
	return luanticonf.Edit(path, set)
}

// ErrUnsafeZip is returned for archives with paths that escape the target.
var ErrUnsafeZip = errors.New("unsafe path in archive")

// Extract unpacks a zip into dst with luanti-depgraph's guard: no absolute paths, no "..",
// no symlink entries, no backslash names outside Windows, and at most maxSize bytes in total.
func Extract(zipPath, dst string, maxSize int64) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening release zip: %w", err)
	}
	defer zr.Close()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	var total int64
	for _, f := range zr.File {
		name := f.Name
		if strings.Contains(name, `\`) {
			return fmt.Errorf("%w: %q", ErrUnsafeZip, name)
		}
		if strings.HasPrefix(name, "/") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
			return fmt.Errorf("%w: %q", ErrUnsafeZip, name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return fmt.Errorf("%w: %q", ErrUnsafeZip, name)
			}
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink %q", ErrUnsafeZip, name)
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		if !fsutil.Within(dst, target) {
			return fmt.Errorf("%w: %q", ErrUnsafeZip, name)
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		n, err := extractFile(f, target, maxSize-total)
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}

func extractFile(f *zip.File, target string, budget int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, budget+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > budget {
		err = errors.New("release is larger than the size limit")
	}
	return n, err
}
