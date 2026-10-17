package depgraph

import "github.com/culmat/luanthetti/internal/luanticonf"

// Target is what gets resolved: a ContentDB package or a local directory.
type Target struct {
	Key      string
	Type     string
	Provides Set
	Depends  Set
	Optional Set
}

// TargetFromPkg builds a target from a package, dropping self-provided names.
func TargetFromPkg(p *Pkg) Target {
	return Target{
		Key:      p.Key(),
		Type:     p.Type,
		Provides: p.Provides.Copy(),
		Depends:  p.Depends.Minus(p.Provides),
		Optional: p.Optional.Minus(p.Provides),
	}
}

// TargetFromScan builds a target from a scanned local package; its key is "local:<name>".
func TargetFromScan(s *luanticonf.ScannedPackage) Target {
	t := Target{Key: "local:" + s.Name, Type: s.Type, Provides: Set{}, Depends: Set{}, Optional: Set{}}
	for n := range s.Provides() {
		t.Provides.Add(n)
	}
	for n := range s.HardDepends() {
		t.Depends.Add(n)
	}
	for n := range s.OptionalDepends() {
		t.Optional.Add(n)
	}
	return t
}

// IsLocal reports whether the target key names a local directory.
func (t Target) IsLocal() bool { return len(t.Key) > 6 && t.Key[:6] == "local:" }
