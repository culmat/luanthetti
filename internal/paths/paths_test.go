package paths

import (
	"path/filepath"
	"testing"
)

func TestHomeOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvHome, dir)
	d, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if d.Data != dir || d.Config != dir || d.Cache != filepath.Join(dir, "cache") {
		t.Errorf("%+v", d)
	}
	if d.StateFile() != filepath.Join(dir, "state.sqlite") || d.LibraryDir() != filepath.Join(dir, "library") {
		t.Errorf("%+v", d)
	}
	flag := t.TempDir()
	d, _ = Resolve(flag)
	if d.Data != flag {
		t.Errorf("flag should win over env: %+v", d)
	}
	if err := d.Ensure(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultIsUnderHome(t *testing.T) {
	t.Setenv(EnvHome, "")
	d, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if d.Data == "" || d.Cache == "" || filepath.Base(d.Data) != "luanthetti" {
		t.Errorf("%+v", d)
	}
}
