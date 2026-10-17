// Package version holds the build information and derives the ContentDB User-Agent from it.
package version

import "runtime/debug"

var (
	version = "dev"
	commit  = ""
	date    = ""
)

// Set records the build information passed in by main.
func Set(v, c, d string) {
	if v != "" {
		version = v
	}
	commit, date = c, d
	if commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" && len(s.Value) >= 7 {
					commit = s.Value[:7]
				}
			}
		}
	}
}

// Version is the semantic version without a leading "v", or "dev".
func Version() string { return version }

// Commit is the short git revision, if known.
func Commit() string { return commit }

// Date is the build date, if known.
func Date() string { return date }

// UserAgent identifies the client to ContentDB, as its etiquette asks.
func UserAgent() string {
	return "luanthetti/" + version + " (+https://github.com/culmat/luanthetti)"
}
