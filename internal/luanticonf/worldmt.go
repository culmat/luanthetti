package luanticonf

import (
	"path/filepath"
	"strings"
)

// WorldMT is the part of a world.mt file that concerns content.
type WorldMT struct {
	Path   string `json:"path"`
	GameID string `json:"gameid"`
	// Mods maps mod name to the load_mod_<name> value: "true", "false", or a path such as
	// "mods/modpack/moddir" (relative to the user path) or an absolute path under LUANTI_MOD_PATH.
	Mods map[string]string `json:"mods"`
	Conf map[string]string `json:"-"`
}

// ReadWorldMT reads worldDir/world.mt.
func ReadWorldMT(worldDir string) WorldMT {
	path := filepath.Join(worldDir, "world.mt")
	conf := ReadConf(path)
	w := WorldMT{Path: path, GameID: conf["gameid"], Mods: map[string]string{}, Conf: conf}
	for k, v := range conf {
		if name, ok := strings.CutPrefix(k, "load_mod_"); ok && name != "" {
			w.Mods[name] = v
		}
	}
	return w
}

// Enabled reports whether the world loads mod `name` (true or a path, as the engine treats both as enabled).
func (w WorldMT) Enabled(name string) bool {
	v, ok := w.Mods[name]
	return ok && v != "false" && v != ""
}
