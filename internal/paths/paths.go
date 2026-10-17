// Package paths locates Luanthetti's data, config and cache directories.
//
// LUANTHETTI_HOME (or --home) collapses all three into one directory, which tests always
// set. Otherwise the per-OS conventions apply: XDG on Linux and other Unixes,
// ~/Library/Application Support and ~/Library/Caches on macOS, %LOCALAPPDATA% on Windows.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// EnvHome is the environment variable that overrides every directory.
const EnvHome = "LUANTHETTI_HOME"

// Dirs holds the resolved locations.
type Dirs struct {
	Data   string // library/, state.sqlite, trash/
	Config string // config.toml
	Cache  string // depgraph/, thumbs/
}

// Resolve returns the directories for the given --home override ("" for the default).
func Resolve(homeFlag string) (Dirs, error) {
	if h := firstNonEmpty(homeFlag, os.Getenv(EnvHome)); h != "" {
		abs, err := filepath.Abs(h)
		if err != nil {
			return Dirs{}, err
		}
		return Dirs{Data: abs, Config: abs, Cache: filepath.Join(abs, "cache")}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Dirs{}, err
	}
	switch runtime.GOOS {
	case "darwin":
		app := filepath.Join(home, "Library", "Application Support", "luanthetti")
		return Dirs{Data: app, Config: app, Cache: filepath.Join(home, "Library", "Caches", "luanthetti")}, nil
	case "windows":
		base := firstNonEmpty(os.Getenv("LOCALAPPDATA"), filepath.Join(home, "AppData", "Local"))
		app := filepath.Join(base, "luanthetti")
		return Dirs{Data: app, Config: app, Cache: filepath.Join(app, "cache")}, nil
	default:
		data := firstNonEmpty(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
		config := firstNonEmpty(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
		cache := firstNonEmpty(os.Getenv("XDG_CACHE_HOME"), filepath.Join(home, ".cache"))
		return Dirs{
			Data:   filepath.Join(data, "luanthetti"),
			Config: filepath.Join(config, "luanthetti"),
			Cache:  filepath.Join(cache, "luanthetti"),
		}, nil
	}
}

// ConfigFile is config.toml.
func (d Dirs) ConfigFile() string { return filepath.Join(d.Config, "config.toml") }

// StateFile is the SQLite database with installations, links, pins and caches.
func (d Dirs) StateFile() string { return filepath.Join(d.Data, "state.sqlite") }

// LibraryDir is where package releases are stored once.
func (d Dirs) LibraryDir() string { return filepath.Join(d.Data, "library") }

// TrashDir holds directories displaced by adopt or repair until gc removes them.
func (d Dirs) TrashDir() string { return filepath.Join(d.Data, "trash") }

// DepgraphDir holds the luanti-depgraph snapshot and its sidecar.
func (d Dirs) DepgraphDir() string { return filepath.Join(d.Cache, "depgraph") }

// ThumbsDir holds cached thumbnails.
func (d Dirs) ThumbsDir() string { return filepath.Join(d.Cache, "thumbs") }

// Ensure creates the directories.
func (d Dirs) Ensure() error {
	for _, p := range []string{d.Data, d.Config, d.Cache, d.LibraryDir(), d.DepgraphDir(), d.ThumbsDir()} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
