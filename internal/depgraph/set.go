package depgraph

import "sort"

// Set is a set of mod names.
type Set map[string]struct{}

// NewSet builds a set from names.
func NewSet(names ...string) Set {
	s := Set{}
	for _, n := range names {
		s[n] = struct{}{}
	}
	return s
}

// Has reports membership.
func (s Set) Has(name string) bool { _, ok := s[name]; return ok }

// Add inserts a name.
func (s Set) Add(name string) { s[name] = struct{}{} }

// Copy returns a shallow copy.
func (s Set) Copy() Set {
	out := make(Set, len(s))
	for k := range s {
		out[k] = struct{}{}
	}
	return out
}

// Union returns s ∪ other as a new set.
func (s Set) Union(other Set) Set {
	out := s.Copy()
	for k := range other {
		out[k] = struct{}{}
	}
	return out
}

// Minus returns s − other as a new set.
func (s Set) Minus(other Set) Set {
	out := Set{}
	for k := range s {
		if !other.Has(k) {
			out[k] = struct{}{}
		}
	}
	return out
}

// Intersect returns s ∩ other as a new set.
func (s Set) Intersect(other Set) Set {
	out := Set{}
	for k := range s {
		if other.Has(k) {
			out[k] = struct{}{}
		}
	}
	return out
}

// Sorted returns the names in byte order (Python's `sorted` for ASCII mod names).
func (s Set) Sorted() []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
