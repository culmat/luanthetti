package thumbs

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type fetcher struct{ calls int }

func (f *fetcher) Get(ctx context.Context, url string) (*http.Response, error) {
	f.calls++
	ct := "image/png"
	if strings.Contains(url, "html") {
		ct = "text/html"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 600)))}, nil
}

func TestCacheFetchesOnceAndEvicts(t *testing.T) {
	f := &fetcher{}
	c := &Cache{Dir: t.TempDir(), MaxBytes: 1000, Fetch: f}
	ctx := context.Background()
	p, ct, err := c.Get(ctx, "https://content.luanti.org/thumbnails/1/a.png")
	if err != nil || ct != "image/png" {
		t.Fatal(err, ct)
	}
	if _, _, err := c.Get(ctx, "https://content.luanti.org/thumbnails/1/a.png"); err != nil || f.calls != 1 {
		t.Errorf("second get fetched again: %d", f.calls)
	}
	c.Get(ctx, "https://content.luanti.org/thumbnails/1/b.png")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("oldest image not evicted")
	}
	if _, _, err := c.Get(ctx, "https://x/html"); err == nil {
		t.Error("non-image cached")
	}
	off := &Cache{Dir: t.TempDir(), Offline: true, Fetch: f}
	if _, _, err := off.Get(ctx, "https://content.luanti.org/thumbnails/1/c.png"); err != ErrMiss {
		t.Errorf("offline: %v", err)
	}
}
