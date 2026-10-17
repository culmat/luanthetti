// Package thumbs caches ContentDB thumbnails on disk so the web UI never hotlinks ContentDB
// and still shows images offline.
package thumbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Fetcher gets a URL (the ContentDB client, which enforces its host whitelist and rate limit).
type Fetcher interface {
	Get(ctx context.Context, url string) (*http.Response, error)
}

// Cache stores images under Dir, at most MaxBytes in total.
type Cache struct {
	Dir      string
	MaxBytes int64
	Fetch    Fetcher
	Offline  bool
}

// ErrMiss means the image is not cached and could not be fetched.
var ErrMiss = errors.New("thumbnail not available")

// Get returns the cached file path and content type for url, fetching it when needed.
func (c *Cache) Get(ctx context.Context, url string) (string, string, error) {
	sum := sha256.Sum256([]byte(url))
	base := filepath.Join(c.Dir, hex.EncodeToString(sum[:16]))
	if ct, err := os.ReadFile(base + ".type"); err == nil {
		if _, err := os.Stat(base); err == nil {
			now := time.Now()
			os.Chtimes(base, now, now)
			return base, string(ct), nil
		}
	}
	if c.Offline || c.Fetch == nil {
		return "", "", ErrMiss
	}
	resp, err := c.Fetch.Get(ctx, url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return "", "", errors.New("not an image: " + ct)
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", "", err
	}
	tmp := base + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", "", err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 5<<20))
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	if err := os.Rename(tmp, base); err != nil {
		return "", "", err
	}
	os.WriteFile(base+".type", []byte(ct), 0o644)
	c.evict()
	return base, ct, nil
}

// evict removes the least recently used images beyond MaxBytes.
func (c *Cache) evict() {
	if c.MaxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if strings.Contains(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{filepath.Join(c.Dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= c.MaxBytes {
			return
		}
		os.Remove(f.path)
		os.Remove(f.path + ".type")
		total -= f.size
	}
}
