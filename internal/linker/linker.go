// Package linker places library folders into installations: a symlink where possible, a
// directory junction on Windows without symlink privilege, a copy as the last resort, or
// nothing at all in "env" mode (the server is started with LUANTI_MOD_PATH instead).
// It never removes anything through a link and never deletes outside the installation.
package linker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/culmat/luanthetti/internal/fsutil"
)

// Kinds of links.
const (
	Symlink  = "symlink"
	Junction = "junction"
	Copy     = "copy"
	Env      = "env"
	Auto     = "auto"
)

// Inspection describes what is at a path.
type Inspection struct {
	Exists   bool   `json:"exists"`
	IsLink   bool   `json:"is_link"` // symlink or junction
	Kind     string `json:"kind,omitempty"`
	Target   string `json:"target,omitempty"`
	Dangling bool   `json:"dangling,omitempty"`
	IsDir    bool   `json:"is_dir"`
}

// Inspect looks at path without following it.
func Inspect(path string) (Inspection, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Inspection{}, nil
	}
	if err != nil {
		return Inspection{}, err
	}
	in := Inspection{Exists: true}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		in.IsLink, in.Kind = true, Symlink
	case isJunction(path, fi):
		in.IsLink, in.Kind = true, Junction
	default:
		in.IsDir = fi.IsDir()
		return in, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return in, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	in.Target = filepath.Clean(target)
	st, err := os.Stat(path)
	in.Dangling = err != nil
	in.IsDir = err == nil && st.IsDir()
	return in, nil
}

// Link makes path point at target (a library folder) using mode, falling back as needed when
// mode is auto. It returns the kind actually used. path must not exist.
func Link(target, path, mode string) (string, error) {
	if in, err := Inspect(path); err != nil {
		return "", err
	} else if in.Exists {
		return "", fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	switch mode {
	case Env:
		return Env, nil
	case Copy:
		return Copy, fsutil.CopyDir(target, path)
	case Symlink:
		return Symlink, os.Symlink(target, path)
	case Junction:
		return Junction, makeJunction(target, path)
	case Auto, "":
		if err := os.Symlink(target, path); err == nil {
			return Symlink, nil
		}
		if err := makeJunction(target, path); err == nil {
			return Junction, nil
		}
		return Copy, fsutil.CopyDir(target, path)
	}
	return "", fmt.Errorf("unknown link mode %q", mode)
}

// Replace points an existing link (or copy) at a new target. For symlinks it creates the new
// link beside the old one and renames it over, which is atomic on POSIX. root is the
// installation root, the boundary for removing copies.
func Replace(root, target, path, kind string) error {
	switch kind {
	case Env:
		return nil
	case Symlink:
		tmp := path + ".luanthetti-new"
		os.Remove(tmp)
		if err := os.Symlink(target, tmp); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			// Windows cannot rename over an existing link: remove, then rename.
			if rmErr := Unlink(root, path, kind); rmErr != nil {
				os.Remove(tmp)
				return err
			}
			return os.Rename(tmp, path)
		}
		return nil
	case Junction:
		if err := Unlink(root, path, kind); err != nil {
			return err
		}
		return makeJunction(target, path)
	case Copy:
		tmp, old := path+".luanthetti-new", path+".luanthetti-old"
		if err := fsutil.RemoveAllWithin(root, tmp); err != nil {
			return err
		}
		if err := fsutil.CopyDir(target, tmp); err != nil {
			fsutil.RemoveAllWithin(root, tmp)
			return err
		}
		fsutil.RemoveAllWithin(root, old)
		if err := os.Rename(path, old); err != nil {
			fsutil.RemoveAllWithin(root, tmp)
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Rename(old, path)
			return err
		}
		return fsutil.RemoveAllWithin(root, old)
	}
	return fmt.Errorf("unknown link kind %q", kind)
}

// Unlink removes a link or a copy at path. A symlink or junction is removed as an entry and
// its target is never touched; a copy is removed only when it is inside root and is a real
// directory. A missing path is not an error.
func Unlink(root, path, kind string) error {
	in, err := Inspect(path)
	if err != nil {
		return err
	}
	if !in.Exists {
		return nil
	}
	if !fsutil.Within(root, path) {
		return fmt.Errorf("refusing to remove %s: outside the installation %s", path, root)
	}
	switch kind {
	case Env:
		return nil
	case Symlink, Junction:
		if !in.IsLink {
			return fmt.Errorf("refusing to remove %s: expected a link, found a real directory (replaced by another tool?)", path)
		}
		return os.Remove(path)
	case Copy:
		if in.IsLink {
			return os.Remove(path)
		}
		return fsutil.RemoveAllWithin(root, path)
	}
	return fmt.Errorf("unknown link kind %q", kind)
}

// Probe finds the best link kind that works in dir by trying it.
func Probe(dir string) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Copy
	}
	target, err := os.MkdirTemp(dir, ".luanthetti-probe-target-")
	if err != nil {
		return Copy
	}
	defer os.RemoveAll(target)
	link := target + "-link"
	defer os.Remove(link)
	if os.Symlink(target, link) == nil {
		return Symlink
	}
	if makeJunction(target, link) == nil {
		return Junction
	}
	return Copy
}

// PointsInto reports whether an inspected link targets something inside dir.
func (in Inspection) PointsInto(dir string) bool {
	return in.IsLink && fsutil.Within(dir, in.Target)
}
