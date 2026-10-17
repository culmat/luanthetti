//go:build !windows

package linker

import (
	"errors"
	"io/fs"
)

var errNoJunctions = errors.New("junctions exist only on Windows")

func makeJunction(target, path string) error { return errNoJunctions }

func isJunction(path string, fi fs.FileInfo) bool { return false }
