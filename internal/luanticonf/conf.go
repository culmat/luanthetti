// Package luanticonf reads and edits Luanti content metadata on disk: game.conf,
// modpack.conf, mod.conf, texture_pack.conf, the legacy depends.txt / modpack.txt, and
// world.mt. It follows the engine's rules (doc/lua_api.md "Mods", src/content/mods.cpp,
// src/settings.cpp) and is a port of luanti-depgraph's luanticonf.py, so both tools
// read a package the same way.
package luanticonf

import (
	"os"
	"regexp"
	"strings"
)

// ModNameRE is the engine's rule for mod names.
var ModNameRE = regexp.MustCompile(`^[a-z0-9_]+$`)

var whitespaceRE = regexp.MustCompile(`\s`)

// ParseConf parses the settings format: `key = value`, `#` comments, `"""` multi-line values.
// Later duplicate keys overwrite earlier ones, as in the engine.
func ParseConf(text string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		i++
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if strings.HasPrefix(value, `"""`) {
			value = value[3:]
			var parts []string
			for !strings.HasSuffix(value, `"""`) && i < len(lines) {
				parts = append(parts, value)
				value = strings.TrimRight(lines[i], "\r")
				i++
			}
			parts = append(parts, strings.TrimSuffix(value, `"""`))
			value = strings.TrimSpace(strings.Join(parts, "\n"))
		}
		out[key] = value
	}
	return out
}

// SplitNames splits a depends-style list: the engine removes all whitespace and splits on commas.
func SplitNames(value string) []string {
	if value == "" {
		return nil
	}
	var out []string
	for _, n := range strings.Split(whitespaceRE.ReplaceAllString(value, ""), ",") {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// SplitGames splits supported_games / unsupported_games; ContentDB ignores a `_game` suffix.
func SplitGames(value string) []string {
	names := SplitNames(value)
	for i, g := range names {
		if g != "*" {
			names[i] = strings.TrimSuffix(g, "_game")
		}
	}
	return names
}

// ReadConf parses the file at path, or returns an empty map if it does not exist.
func ReadConf(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	return ParseConf(string(data))
}

// Edit sets keys in a conf file the way the engine's Settings::updateConfigFile does:
// existing `key = value` lines are replaced in place (multi-line values included),
// unknown keys are appended, and every other line (comments, blank lines, other keys)
// is kept byte for byte. The file is created when missing. Keys in `set` with values
// that contain a newline are written with `"""` quotes.
func Edit(path string, set map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(data)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	// A trailing newline produces an empty last element; drop it and add it back at the end.
	trailingNL := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailingNL {
		lines = lines[:len(lines)-1]
	}

	done := map[string]bool{}
	var out []string
	for i := 0; i < len(lines); {
		raw := lines[i]
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			out = append(out, raw)
			i++
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		end := i + 1 // first line after this setting
		if strings.HasPrefix(value, `"""`) && !strings.HasSuffix(value[3:], `"""`) {
			for end < len(lines) && !strings.HasSuffix(strings.TrimRight(lines[end], "\r"), `"""`) {
				end++
			}
			if end < len(lines) {
				end++
			}
		}
		if v, ok := set[key]; ok && !done[key] {
			out = append(out, formatSetting(key, v, nl))
			done[key] = true
		} else if ok && done[key] {
			// A duplicate of a key we already wrote: drop it, so the file has one value.
		} else {
			out = append(out, lines[i:end]...)
		}
		i = end
	}
	for _, key := range sortedKeys(set) {
		if !done[key] {
			out = append(out, formatSetting(key, set[key], nl))
		}
	}
	result := strings.Join(out, nl)
	if len(out) > 0 {
		result += nl
	}
	return os.WriteFile(path, []byte(result), 0o644)
}

func formatSetting(key, value, nl string) string {
	if strings.Contains(value, "\n") {
		value = strings.ReplaceAll(value, "\n", nl)
		return key + ` = """` + nl + value + nl + `"""`
	}
	return key + " = " + value
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// insertion sort keeps the dependency list empty; the maps are tiny
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
