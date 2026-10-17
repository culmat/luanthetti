// Package fake is an in-memory ContentDB for tests: packages, releases, zips and updates served
// over httptest with the real API's shapes.
package fake

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/culmat/luanthetti/internal/contentdb"
)

// Package is a package on the fake server.
type Package struct {
	contentdb.Detail
	Releases []contentdb.Release
	Zips     map[int64][]byte // release id → zip
}

// Server is the fake ContentDB.
type Server struct {
	mu       sync.Mutex
	packages map[string]*Package // key
	Tags     []contentdb.Tag
	Requests []string       // request paths, for assertions
	Fail     map[string]int // path prefix → HTTP status to answer with (once per hit counter)
	*httptest.Server
}

// New starts an empty fake server.
func New() *Server {
	s := &Server{packages: map[string]*Package{}, Fail: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Add registers a package with releases; the newest release is the package's current one.
func (s *Server) Add(p *Package) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Zips == nil {
		p.Zips = map[int64][]byte{}
	}
	if len(p.Releases) > 0 {
		sort.Slice(p.Releases, func(i, j int) bool { return p.Releases[i].ID > p.Releases[j].ID })
		p.Release = p.Releases[0].ID
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.ContentWarnings == nil {
		p.ContentWarnings = []string{}
	}
	if p.Screenshots == nil {
		p.Screenshots = []string{}
	}
	s.packages[p.Key()] = p
}

// Get returns a registered package.
func (s *Server) Get(author, name string) *Package {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.packages[strings.ToLower(author)+"/"+name]
}

// AddRelease appends a release with a zip and makes it current.
func (s *Server) AddRelease(author, name string, id int64, title string, zipData []byte) {
	s.mu.Lock()
	p := s.packages[strings.ToLower(author)+"/"+name]
	s.mu.Unlock()
	p.Releases = append(p.Releases, contentdb.Release{ID: id, Title: title, Name: title, ReleaseDate: "2026-10-17T00:00:00",
		URL: fmt.Sprintf("/uploads/%d.zip", id)})
	p.Zips[id] = zipData
	s.Add(p)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Requests = append(s.Requests, r.URL.RequestURI())
	for prefix, status := range s.Fail {
		if strings.HasPrefix(r.URL.Path, prefix) {
			delete(s.Fail, prefix)
			s.mu.Unlock()
			w.WriteHeader(status)
			return
		}
	}
	s.mu.Unlock()
	if r.Header.Get("User-Agent") == "" || strings.HasPrefix(r.Header.Get("User-Agent"), "Go-http-client") {
		http.Error(w, "identify yourself", http.StatusForbidden)
		return
	}
	path := r.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/api/packages/":
		s.list(w, r)
	case path == "/api/updates/":
		s.updates(w)
	case path == "/api/tags/":
		writeJSON(w, s.Tags)
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "packages":
		if p := s.Get(parts[2], parts[3]); p != nil {
			writeJSON(w, p.Detail)
			return
		}
		http.NotFound(w, r)
	case len(parts) == 5 && parts[0] == "api" && parts[1] == "packages" && parts[4] == "releases":
		if p := s.Get(parts[2], parts[3]); p != nil {
			writeJSON(w, p.Releases)
			return
		}
		http.NotFound(w, r)
	case len(parts) == 6 && parts[0] == "packages" && parts[3] == "releases" && parts[5] == "download":
		id, _ := strconv.ParseInt(parts[4], 10, 64)
		if p := s.Get(parts[1], parts[2]); p != nil {
			if z, ok := p.Zips[id]; ok {
				w.Header().Set("Content-Type", "application/zip")
				w.Write(z)
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasPrefix(path, "/thumbnails/"):
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG fake " + path))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	var pkgs []*Package
	for _, p := range s.packages {
		pkgs = append(pkgs, p)
	}
	s.mu.Unlock()
	types := q["type"]
	tags := q["tag"]
	search := strings.ToLower(q.Get("q"))
	var out []contentdb.ShortPackage
	for _, p := range pkgs {
		if len(types) > 0 && !contains(types, p.Type) {
			continue
		}
		if !hasAll(p.Tags, tags) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(p.Title+" "+p.Name+" "+p.ShortDescription), search) {
			continue
		}
		out = append(out, contentdb.ShortPackage{Author: p.Author, Name: p.Name, Title: p.Title, ShortDescription: p.ShortDescription,
			Type: p.Type, Release: p.Release, Thumbnail: p.Thumbnail})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if limit, _ := strconv.Atoi(q.Get("limit")); limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []contentdb.ShortPackage{}
	}
	writeJSON(w, out)
}

func (s *Server) updates(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int64{}
	for _, p := range s.packages {
		out[p.Author+"/"+p.Name] = p.Release
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !contains(have, w) {
			return false
		}
	}
	return true
}

// ZipDir zips a directory the way ContentDB release zips look: entries under one top folder.
func ZipDir(dir, topFolder string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(filepath.Join(topFolder, rel))
		if d.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ZipFiles builds a zip from a name → content map (for hand-made edge cases).
func ZipFiles(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, _ := zw.Create(n)
		f.Write([]byte(files[n]))
	}
	zw.Close()
	return buf.Bytes()
}
