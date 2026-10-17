package luanticonf

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScannedMod is one mod inside a package.
type ScannedMod struct {
	Name             string   `json:"name"`
	Path             string   `json:"path"` // relative to the package root, slash-separated, "" if the root is the mod
	Dir              string   `json:"-"`
	Title            string   `json:"title,omitempty"`
	Description      string   `json:"description,omitempty"`
	Depends          []string `json:"depends"`
	OptionalDepends  []string `json:"optional_depends"`
	HasModConf       bool     `json:"has_mod_conf"`
	LegacyDependsTxt bool     `json:"legacy_depends_txt,omitempty"`
}

// ScannedPackage is a mod, modpack, game or texture pack read from disk.
type ScannedPackage struct {
	Type             string            `json:"type"` // mod | modpack | game | txp | unknown
	Name             string            `json:"name"`
	Root             string            `json:"root"`
	Title            string            `json:"title,omitempty"`
	Description      string            `json:"description,omitempty"`
	Mods             []ScannedMod      `json:"mods"`
	SupportedGames   []string          `json:"supported_games"`
	UnsupportedGames []string          `json:"unsupported_games"`
	Conf             map[string]string `json:"conf"`
}

// Provides is the set of mod names the package contains.
func (p *ScannedPackage) Provides() map[string]bool {
	out := map[string]bool{}
	for _, m := range p.Mods {
		out[m.Name] = true
	}
	return out
}

// HardDepends is the union of the mods' hard dependencies minus what the package provides.
func (p *ScannedPackage) HardDepends() map[string]bool {
	provides := p.Provides()
	out := map[string]bool{}
	for _, m := range p.Mods {
		for _, d := range m.Depends {
			if !provides[d] {
				out[d] = true
			}
		}
	}
	return out
}

// OptionalDepends is the union of optional dependencies minus provides and hard dependencies.
func (p *ScannedPackage) OptionalDepends() map[string]bool {
	provides, hard := p.Provides(), p.HardDepends()
	out := map[string]bool{}
	for _, m := range p.Mods {
		for _, d := range m.OptionalDepends {
			if !provides[d] && !hard[d] {
				out[d] = true
			}
		}
	}
	return out
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// IsModpack reports whether dir holds modpack.conf or the legacy modpack.txt.
func IsModpack(dir string) bool {
	return isFile(filepath.Join(dir, "modpack.conf")) || isFile(filepath.Join(dir, "modpack.txt"))
}

// IsMod reports whether dir holds init.lua or mod.conf.
func IsMod(dir string) bool {
	return isFile(filepath.Join(dir, "init.lua")) || isFile(filepath.Join(dir, "mod.conf"))
}

// IsGame reports whether dir holds game.conf.
func IsGame(dir string) bool { return isFile(filepath.Join(dir, "game.conf")) }

// IsTexturePack reports whether dir holds texture_pack.conf.
func IsTexturePack(dir string) bool { return isFile(filepath.Join(dir, "texture_pack.conf")) }

// subdirs lists the sub-directories of dir, sorted, without dot-directories.
// Symlinked directories count (the engine follows them).
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if e.IsDir() || (e.Type()&os.ModeSymlink != 0 && isDir(p)) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// ReadMod reads one mod directory. root is the package root, for the relative path.
func ReadMod(modDir, root string) ScannedMod {
	conf := ReadConf(filepath.Join(modDir, "mod.conf"))
	rel, err := filepath.Rel(root, modDir)
	if err != nil || rel == "." {
		rel = ""
	}
	mod := ScannedMod{
		Name:            conf["name"],
		Path:            filepath.ToSlash(rel),
		Dir:             modDir,
		Title:           conf["title"],
		Description:     conf["description"],
		Depends:         []string{},
		OptionalDepends: []string{},
		HasModConf:      isFile(filepath.Join(modDir, "mod.conf")),
	}
	if mod.Name == "" {
		mod.Name = filepath.Base(modDir)
	}
	_, hasDep := conf["depends"]
	_, hasOpt := conf["optional_depends"]
	if hasDep || hasOpt {
		mod.Depends = append(mod.Depends, SplitNames(conf["depends"])...)
		mod.OptionalDepends = append(mod.OptionalDepends, SplitNames(conf["optional_depends"])...)
	} else if data, err := os.ReadFile(filepath.Join(modDir, "depends.txt")); err == nil {
		mod.LegacyDependsTxt = true
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasSuffix(line, "?") {
				mod.OptionalDepends = append(mod.OptionalDepends, line[:len(line)-1])
			} else {
				mod.Depends = append(mod.Depends, line)
			}
		}
	}
	if mod.Description == "" {
		if data, err := os.ReadFile(filepath.Join(modDir, "description.txt")); err == nil {
			mod.Description = strings.TrimSpace(string(data))
		}
	}
	return mod
}

// CollectMods lists the mods in a mods directory or modpack, recursing into nested modpacks.
func CollectMods(dir, root string) []ScannedMod {
	var mods []ScannedMod
	for _, sub := range subdirs(dir) {
		switch {
		case IsModpack(sub):
			mods = append(mods, CollectMods(sub, root)...)
		case IsMod(sub):
			mods = append(mods, ReadMod(sub, root))
		}
	}
	return mods
}

// FindContentRoot descends through single sub-directories (repos and zips sometimes wrap the
// content in one folder) until it finds a game, modpack, mod or texture pack, at most depth levels.
func FindContentRoot(path string, depth int) string {
	if IsGame(path) || IsModpack(path) || IsMod(path) || IsTexturePack(path) || depth == 0 {
		return path
	}
	subs := subdirs(path)
	if len(subs) == 1 {
		return FindContentRoot(subs[0], depth-1)
	}
	return path
}

// ScanPackage reads a mod, modpack, game or texture pack at path.
func ScanPackage(path string) (*ScannedPackage, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if !isDir(abs) {
		return nil, &os.PathError{Op: "scan", Path: path, Err: os.ErrNotExist}
	}
	root := FindContentRoot(abs, 2)
	pkg := &ScannedPackage{Root: root, Mods: []ScannedMod{}, Conf: map[string]string{}}
	switch {
	case IsGame(root):
		pkg.Type = "game"
		pkg.Conf = ReadConf(filepath.Join(root, "game.conf"))
		pkg.Name = strings.TrimSuffix(filepath.Base(root), "_game")
		if isDir(filepath.Join(root, "mods")) {
			pkg.Mods = append(pkg.Mods, CollectMods(filepath.Join(root, "mods"), root)...)
		}
	case IsModpack(root):
		pkg.Type = "modpack"
		pkg.Conf = ReadConf(filepath.Join(root, "modpack.conf"))
		pkg.Name = pkg.Conf["name"]
		if pkg.Name == "" {
			pkg.Name = filepath.Base(root)
		}
		pkg.Mods = append(pkg.Mods, CollectMods(root, root)...)
	case IsMod(root):
		mod := ReadMod(root, root)
		pkg.Type = "mod"
		pkg.Conf = ReadConf(filepath.Join(root, "mod.conf"))
		pkg.Name = mod.Name
		pkg.Mods = append(pkg.Mods, mod)
	case IsTexturePack(root):
		pkg.Type = "txp"
		pkg.Conf = ReadConf(filepath.Join(root, "texture_pack.conf"))
		pkg.Name = pkg.Conf["name"]
		if pkg.Name == "" {
			pkg.Name = filepath.Base(root)
		}
	default:
		// A repo with several mods but no modpack.conf: treat it like a modpack.
		pkg.Type = "unknown"
		pkg.Name = filepath.Base(root)
		pkg.Mods = append(pkg.Mods, CollectMods(root, root)...)
		if len(pkg.Mods) > 0 {
			pkg.Type = "modpack"
		}
	}
	pkg.Title = pkg.Conf["title"]
	pkg.Description = pkg.Conf["description"]
	pkg.SupportedGames = emptyIfNil(SplitGames(pkg.Conf["supported_games"]))
	pkg.UnsupportedGames = emptyIfNil(SplitGames(pkg.Conf["unsupported_games"]))
	return pkg, nil
}

// ConfFile returns the metadata file name for a folder type (mod, modpack, game, txp).
func ConfFile(folderType string) string {
	switch folderType {
	case "modpack":
		return "modpack.conf"
	case "game":
		return "game.conf"
	case "txp":
		return "texture_pack.conf"
	default:
		return "mod.conf"
	}
}

// FolderType classifies a content directory as game, modpack, mod, txp or "" (unknown).
// The order matches the engine's pkgmgr.get_base_folder.
func FolderType(dir string) string {
	switch {
	case IsGame(dir):
		return "game"
	case IsModpack(dir):
		return "modpack"
	case IsMod(dir):
		return "mod"
	case IsTexturePack(dir):
		return "txp"
	}
	return ""
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
