package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Library.KeepVersions != 2 || cfg.ContentDB.URL != DefaultContentDBURL || cfg.UI.Port != 7331 {
		t.Errorf("%+v", cfg)
	}
}

func TestRoundTripAndPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.Links.Mode = "copy"
	cfg.Depgraph.AutoRefresh = Duration{time.Hour}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `auto_refresh = "1h0m0s"`) || !strings.Contains(string(data), `mode = "copy"`) {
		t.Errorf("saved:\n%s", data)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Links.Mode != "copy" || back.Depgraph.AutoRefresh.Duration != time.Hour {
		t.Errorf("%+v", back)
	}
	os.WriteFile(path, []byte("[ui]\nport = 9000\n"), 0o644)
	partial, err := Load(path)
	if err != nil || partial.UI.Port != 9000 || partial.Library.KeepVersions != 2 {
		t.Errorf("%v %+v", err, partial)
	}
	os.WriteFile(path, []byte("[links]\nmode = \"hardlink\"\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("invalid mode accepted")
	}
	os.WriteFile(path, []byte("[contentdb]\nrequests_per_second = 10\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("rate above 2/s accepted")
	}
}
