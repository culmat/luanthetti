// Package config loads and saves config.toml.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultSnapshotURL is luanti-depgraph's nightly database.
const DefaultSnapshotURL = "https://github.com/culmat/luanti-depgraph/releases/download/snapshot/luanti-deps.sqlite.zst"

// DefaultContentDBURL is the public ContentDB instance.
const DefaultContentDBURL = "https://content.luanti.org"

// Config is the user configuration.
type Config struct {
	Library   Library   `toml:"library"`
	Links     Links     `toml:"links"`
	ContentDB ContentDB `toml:"contentdb"`
	Depgraph  Depgraph  `toml:"depgraph"`
	UI        UI        `toml:"ui"`
}

// Library configures the package store.
type Library struct {
	Path         string `toml:"path" comment:"default: <data>/library"`
	KeepVersions int    `toml:"keep_versions" comment:"old versions kept per package beyond those linked"`
}

// Links configures how packages are placed into installations.
type Links struct {
	Mode string `toml:"mode" comment:"auto|symlink|junction|copy|env"`
}

// ContentDB configures the API client.
type ContentDB struct {
	URL               string   `toml:"url"`
	RequestsPerSecond float64  `toml:"requests_per_second"`
	UpdatesTTL        Duration `toml:"updates_ttl"`
}

// Depgraph configures the luanti-depgraph snapshot.
type Depgraph struct {
	SnapshotURL string   `toml:"snapshot_url"`
	AutoRefresh Duration `toml:"auto_refresh" comment:"probe for a newer snapshot at most this often; 0 disables"`
}

// UI configures `luanthetti ui`.
type UI struct {
	Port        int  `toml:"port" comment:"0 picks a free port"`
	OpenBrowser bool `toml:"open_browser"`
}

// Duration is a time.Duration that marshals as "24h".
type Duration struct{ time.Duration }

// UnmarshalText parses "24h"-style values.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// MarshalText formats the duration.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.Duration.String()), nil }

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Library:   Library{KeepVersions: 2},
		Links:     Links{Mode: "auto"},
		ContentDB: ContentDB{URL: DefaultContentDBURL, RequestsPerSecond: 2, UpdatesTTL: Duration{24 * time.Hour}},
		Depgraph:  Depgraph{SnapshotURL: DefaultSnapshotURL, AutoRefresh: Duration{20 * time.Hour}},
		UI:        UI{Port: 7331, OpenBrowser: true},
	}
}

// Load reads path over the defaults. A missing file yields the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, cfg.validate()
}

// Save writes the configuration to path.
func Save(path string, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c Config) validate() error {
	switch c.Links.Mode {
	case "auto", "symlink", "junction", "copy", "env":
	default:
		return errors.New("config: links.mode must be auto, symlink, junction, copy or env")
	}
	if c.Library.KeepVersions < 0 {
		return errors.New("config: library.keep_versions must be >= 0")
	}
	if c.ContentDB.RequestsPerSecond <= 0 || c.ContentDB.RequestsPerSecond > 2 {
		return errors.New("config: contentdb.requests_per_second must be between 0 and 2 (ContentDB etiquette)")
	}
	return nil
}
