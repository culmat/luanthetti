//go:build windows

package linker

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

// makeJunction creates an NTFS directory junction, which needs no symlink privilege.
func makeJunction(target, path string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	out, err := exec.Command("cmd", "/c", "mklink", "/J", path, abs).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J: %v: %s", err, out)
	}
	return nil
}

// isJunction: since Go 1.23, Lstat reports mount points (junctions) as ModeIrregular instead
// of ModeSymlink; Readlink still resolves them.
func isJunction(path string, fi fs.FileInfo) bool {
	if fi.Mode()&fs.ModeIrregular == 0 && !fi.IsDir() {
		return false
	}
	_, err := os.Readlink(path)
	return err == nil
}
