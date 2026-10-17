// Package installs finds Luanti installations on this machine and reads what is in them.
//
// An installation is a Luanti user path (or a RUN_IN_PLACE build directory, or a server root)
// with mods/, games/, textures/ and worlds/. The candidates and the RUN_IN_PLACE rule follow
// the engine (README.md "Paths", porting::getUserPathEnvVar) and luanti-org/luma
// (internal/engine/dir.go, MIT).
package installs

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Candidate is a directory that looks like a Luanti installation.
type Candidate struct {
	Root          string   `json:"root"`
	Kind          string   `json:"kind"` // user | run_in_place | flatpak | server
	Source        string   `json:"source"`
	EngineVersion string   `json:"engine_version,omitempty"`
	EngineSource  string   `json:"engine_source,omitempty"` // binary | unknown
	BinaryPath    string   `json:"binary_path,omitempty"`
	ExtraGameDirs []string `json:"extra_game_dirs"`
	Games         []Game   `json:"games"`
	Mods          int      `json:"mods"`
	Exists        bool     `json:"exists"`
}

// Binary is a Luanti executable and what it reports about itself.
type Binary struct {
	Path       string
	Version    string
	RunInPlace bool
}

// binaryNames are tried in this order (server-only builds have luantiserver; minetest* last).
var binaryNames = []string{"luanti", "luantiserver", "minetest", "minetestserver"}

// LooksLikeInstallation reports whether dir has any of the content folders or minetest.conf.
func LooksLikeInstallation(dir string) bool {
	for _, n := range []string{"mods", "games", "worlds", "textures", "minetest.conf"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

// UserPathCandidates lists the per-OS user paths, most likely first.
func UserPathCandidates() []string {
	var out []string
	for _, env := range []string{"LUANTI_USER_PATH", "MINETEST_USER_PATH"} {
		if p := os.Getenv(env); p != "" {
			out = append(out, p)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	switch runtime.GOOS {
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			out = append(out, filepath.Join(appdata, "Luanti"), filepath.Join(appdata, "Minetest"))
		}
	case "darwin":
		out = append(out, filepath.Join(home, "Library", "Application Support", "minetest"),
			filepath.Join(home, "Library", "Application Support", "luanti"))
	default:
		out = append(out, filepath.Join(home, ".minetest"), filepath.Join(home, ".luanti"))
	}
	return out
}

// FlatpakCandidates lists the flatpak user paths (only meaningful on Linux).
func FlatpakCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil || runtime.GOOS != "linux" {
		return nil
	}
	return []string{
		filepath.Join(home, ".var", "app", "org.luanti.luanti", ".minetest"),
		filepath.Join(home, ".var", "app", "net.minetest.Minetest", ".minetest"),
	}
}

// SystemGameDirs lists share-path games folders that exist (system-wide installs).
func SystemGameDirs() []string {
	var out []string
	if runtime.GOOS == "linux" {
		for _, d := range []string{"/usr/share/luanti/games", "/usr/share/minetest/games", "/usr/local/share/luanti/games", "/usr/local/share/minetest/games"} {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				out = append(out, d)
			}
		}
	}
	return out
}

// FindBinaries looks for Luanti executables on PATH and in the usual app locations.
func FindBinaries(ctx context.Context) []Binary {
	seen := map[string]bool{}
	var paths []string
	for _, n := range binaryNames {
		if p, err := exec.LookPath(n); err == nil {
			paths = append(paths, p)
		}
	}
	if runtime.GOOS == "darwin" {
		for _, app := range []string{"/Applications/Luanti.app/Contents/MacOS/luanti", "/Applications/Minetest.app/Contents/MacOS/minetest"} {
			paths = append(paths, app)
		}
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, "Applications", "Luanti.app", "Contents", "MacOS", "luanti"))
		}
	}
	var out []Binary
	for _, p := range paths {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || seen[real] {
			continue
		}
		seen[real] = true
		if b, err := ReadBinary(ctx, p); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// ReadBinary runs `<path> --version` and parses the version and RUN_IN_PLACE.
func ReadBinary(ctx context.Context, path string) (Binary, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return Binary{}, err
	}
	b := Binary{Path: path}
	b.Version, b.RunInPlace = ParseVersionOutput(string(out))
	if b.Version == "" {
		return Binary{}, os.ErrInvalid
	}
	return b, nil
}

// ParseVersionOutput reads "Luanti 5.17.0 (OSX)" (or "Minetest 5.x") and "RUN_IN_PLACE=1".
func ParseVersionOutput(out string) (version string, runInPlace bool) {
	sc := bufio.NewScanner(strings.NewReader(out))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			fields := strings.Fields(line)
			if len(fields) >= 2 && (strings.EqualFold(fields[0], "luanti") || strings.EqualFold(fields[0], "minetest")) {
				version = strings.TrimSuffix(fields[1], "-dev")
				if strings.HasSuffix(fields[1], "-dev") {
					version = fields[1]
				}
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "RUN_IN_PLACE="); ok {
			runInPlace = v == "1"
		}
	}
	return version, runInPlace
}

// RunInPlaceRoot is the build directory of a RUN_IN_PLACE binary: bin/<exe> or
// bin/<CMakeConfig>/<exe>.
func RunInPlaceRoot(binPath string) string {
	dir := filepath.Dir(binPath)
	if filepath.Base(dir) != "bin" {
		if parent := filepath.Dir(dir); filepath.Base(parent) == "bin" {
			dir = parent
		}
	}
	return filepath.Dir(dir)
}

// Detect lists installation candidates: user paths, flatpak paths and RUN_IN_PLACE builds of
// binaries on PATH. Only existing directories that look like installations are returned
// unless all is true.
func Detect(ctx context.Context, all bool) []Candidate {
	bins := FindBinaries(ctx)
	var system *Binary
	for i := range bins {
		if !bins[i].RunInPlace {
			system = &bins[i]
			break
		}
	}
	seen := map[string]bool{}
	var out []Candidate
	add := func(root, kind, source string, bin *Binary) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		c := Inspect(abs)
		c.Kind, c.Source = kind, source
		if bin != nil {
			c.EngineVersion, c.EngineSource, c.BinaryPath = bin.Version, "binary", bin.Path
		} else {
			c.EngineSource = "unknown"
		}
		if kind == "user" {
			c.ExtraGameDirs = SystemGameDirs()
			c.Games = append(c.Games, gamesIn(c.ExtraGameDirs...)...)
		}
		if c.Exists || all {
			out = append(out, c)
		}
	}
	for _, p := range UserPathCandidates() {
		add(p, "user", "user path", system)
	}
	for _, p := range FlatpakCandidates() {
		add(p, "flatpak", "flatpak", nil)
	}
	for i := range bins {
		if bins[i].RunInPlace {
			add(RunInPlaceRoot(bins[i].Path), "run_in_place", "RUN_IN_PLACE build of "+bins[i].Path, &bins[i])
		}
	}
	return out
}

// Inspect reads an installation directory: games and number of top-level mods.
func Inspect(root string) Candidate {
	c := Candidate{Root: root, ExtraGameDirs: []string{}, Games: []Game{}}
	c.Exists = LooksLikeInstallation(root)
	if !c.Exists {
		return c
	}
	c.Games = gamesIn(filepath.Join(root, "games"))
	if entries, err := os.ReadDir(filepath.Join(root, "mods")); err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") && (e.IsDir() || e.Type()&os.ModeSymlink != 0) {
				c.Mods++
			}
		}
	}
	// A RUN_IN_PLACE build has its binary in bin/.
	for _, n := range binaryNames {
		for _, name := range []string{n, n + ".exe"} {
			p := filepath.Join(root, "bin", name)
			if _, err := os.Stat(p); err == nil {
				if b, err := ReadBinary(context.Background(), p); err == nil {
					c.EngineVersion, c.EngineSource, c.BinaryPath = b.Version, "binary", p
					if b.RunInPlace {
						c.Kind = "run_in_place"
					}
					return c
				}
			}
		}
	}
	return c
}

// SortCandidates orders candidates by kind (user first) then root.
func SortCandidates(cs []Candidate) {
	order := map[string]int{"user": 0, "flatpak": 1, "run_in_place": 2, "server": 3}
	sort.SliceStable(cs, func(i, j int) bool {
		if order[cs[i].Kind] != order[cs[j].Kind] {
			return order[cs[i].Kind] < order[cs[j].Kind]
		}
		return cs[i].Root < cs[j].Root
	})
}
