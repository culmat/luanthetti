// Package fsutil has the few file operations several packages share: copying trees,
// checking that a path stays inside a root, and moving directories across volumes.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Within reports whether path is root or inside it (both cleaned, not resolved).
func Within(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// SkipNames are left out when copying package trees, as luanti-depgraph does.
var SkipNames = map[string]bool{".git": true, ".github": true, ".ldg-release": true}

// CopyDir copies src to dst (which must not exist). Symlinks inside src are not followed;
// they are recreated as symlinks when they point inside src and skipped otherwise.
func CopyDir(src, dst string) error {
	src = filepath.Clean(src)
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel != "." && SkipNames[d.Name()] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			abs := link
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(filepath.Dir(path), link)
			}
			if !Within(src, abs) {
				return nil
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(path, target, info.Mode().Perm())
		}
		return nil
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// MoveDir renames src to dst, falling back to copy-then-remove across volumes.
func MoveDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) {
		return err
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		return fmt.Errorf("move %s: %s already exists", src, dst)
	}
	if err := CopyDir(src, dst); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// RemoveAllWithin removes path only if it is inside root, as a guard against removing the
// wrong tree through a bad record.
func RemoveAllWithin(root, path string) error {
	if !Within(root, path) || filepath.Clean(root) == filepath.Clean(path) {
		return fmt.Errorf("refusing to remove %s: not inside %s", path, root)
	}
	return os.RemoveAll(path)
}
