package installs

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/culmat/luanthetti/internal/luanticonf"
)

// Game is a game folder in an installation.
type Game struct {
	ID      string `json:"id"` // folder name minus "_game", the engine's game id
	Folder  string `json:"folder"`
	Path    string `json:"path"`
	Title   string `json:"title,omitempty"`
	Author  string `json:"author,omitempty"`
	Release int64  `json:"release,omitempty"`
}

// ContentDBKey is the in-game client's identity for an installed game: lower(author)/id,
// only for games installed from ContentDB (release > 0).
func (g Game) ContentDBKey() string {
	if g.Author == "" || g.Release <= 0 {
		return ""
	}
	return strings.ToLower(g.Author) + "/" + g.ID
}

// NormalizeGameID strips a trailing "_game", as the engine does (subgames.cpp normalizeGameId).
func NormalizeGameID(name string) string { return strings.TrimSuffix(name, "_game") }

// Games lists the games of an installation: root/games plus extra game directories.
func Games(root string, extra ...string) []Game {
	return gamesIn(append([]string{filepath.Join(root, "games")}, extra...)...)
}

func gamesIn(dirs ...string) []Game {
	out := []Game{}
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if !luanticonf.IsGame(p) {
				continue
			}
			id := NormalizeGameID(e.Name())
			if seen[id] {
				continue // the user's copy (listed first) wins, as in the engine
			}
			seen[id] = true
			conf := luanticonf.ReadConf(filepath.Join(p, "game.conf"))
			rel, _ := strconv.ParseInt(conf["release"], 10, 64)
			title := conf["title"]
			if title == "" {
				title = conf["name"]
			}
			out = append(out, Game{ID: id, Folder: e.Name(), Path: p, Title: title, Author: conf["author"], Release: rel})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// World is a world folder and its world.mt.
type World struct {
	Name string             `json:"name"`
	Path string             `json:"path"`
	MT   luanticonf.WorldMT `json:"world_mt"`
}

// Worlds lists root/worlds/*/world.mt.
func Worlds(root string) []World {
	entries, err := os.ReadDir(filepath.Join(root, "worlds"))
	if err != nil {
		return nil
	}
	var out []World
	for _, e := range entries {
		p := filepath.Join(root, "worlds", e.Name())
		if _, err := os.Stat(filepath.Join(p, "world.mt")); err != nil {
			continue
		}
		mt := luanticonf.ReadWorldMT(p)
		name := mt.Conf["world_name"]
		if name == "" {
			name = e.Name()
		}
		out = append(out, World{Name: name, Path: p, MT: mt})
	}
	return out
}

// ContentDir is the folder of an installation that holds packages of a folder type.
func ContentDir(root, folderType string) string {
	switch folderType {
	case "game":
		return filepath.Join(root, "games")
	case "txp":
		return filepath.Join(root, "textures")
	default:
		return filepath.Join(root, "mods")
	}
}
