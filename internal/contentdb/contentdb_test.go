package contentdb_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/culmat/luanthetti/internal/contentdb"
	"github.com/culmat/luanthetti/internal/contentdb/fake"
)

func newClient(t *testing.T) (*fake.Server, *contentdb.Client) {
	t.Helper()
	s := fake.New()
	t.Cleanup(s.Close)
	c := contentdb.New(s.URL, "luanthetti-test/0 (+test)", 2)
	c.HTTP = s.Client()
	c.Backoff = 10 * time.Millisecond
	return s, c
}

func TestEndpoints(t *testing.T) {
	s, c := newClient(t)
	c.SetLimit(1000) // local fake server
	zipData, err := fake.ZipDir("../../testdata/packages/legacy_mod", "legacy_mod")
	if err != nil {
		t.Fatal(err)
	}
	p := &fake.Package{}
	p.Author, p.Name, p.Title, p.Type, p.ShortDescription = "TenPlus1", "legacy_mod", "Legacy", "mod", "An old mod"
	p.Tags = []string{"farming"}
	p.DevState = "maintenance_only"
	s.Add(p)
	s.AddRelease("TenPlus1", "legacy_mod", 100, "1.0", zipData)
	s.Add(&fake.Package{Detail: contentdb.Detail{Author: "x", Name: "game1", Title: "G", Type: "game"}})
	s.Tags = []contentdb.Tag{{Name: "farming", Title: "Farming"}}

	ctx := context.Background()
	list, err := c.ListPackages(ctx, contentdb.ListQuery{Types: []string{"mod"}, Tags: []string{"farming"}, Q: "legacy"})
	if err != nil || len(list) != 1 || list[0].Release != 100 || list[0].Key() != "tenplus1/legacy_mod" {
		t.Fatalf("%v %+v", err, list)
	}
	if list, _ := c.ListPackages(ctx, contentdb.ListQuery{Types: []string{"txp"}}); len(list) != 0 {
		t.Error("type filter")
	}
	d, err := c.Package(ctx, "TenPlus1", "legacy_mod")
	if err != nil || d.DevState != "maintenance_only" || d.Release != 100 || d.Tags[0] != "farming" {
		t.Fatalf("%v %+v", err, d)
	}
	if _, err := c.Package(ctx, "no", "body"); !contentdb.IsNotFound(err) {
		t.Errorf("404: %v", err)
	}
	rels, err := c.Releases(ctx, "TenPlus1", "legacy_mod")
	if err != nil || len(rels) != 1 || rels[0].ID != 100 || rels[0].Title != "1.0" {
		t.Fatalf("%v %+v", err, rels)
	}
	up, err := c.LatestReleases(ctx, "5.17.0", 48)
	if err != nil || up["tenplus1/legacy_mod"] != 100 {
		t.Fatalf("%v %v", err, up)
	}
	tags, _ := c.Tags(ctx)
	if len(tags) != 1 || tags[0].Name != "farming" {
		t.Error("tags")
	}
	var buf bytes.Buffer
	n, err := c.Download(ctx, "TenPlus1", "legacy_mod", 100, &buf)
	if err != nil || n != int64(len(zipData)) || !bytes.Equal(buf.Bytes(), zipData) {
		t.Fatalf("download: %v %d", err, n)
	}
	if !strings.HasSuffix(c.DownloadURL("A", "b", 7), "/packages/A/b/releases/7/download/") {
		t.Error(c.DownloadURL("A", "b", 7))
	}
	resp, err := c.Get(ctx, s.URL+"/thumbnails/1/x.png")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("thumb: %v", err)
	}
	resp.Body.Close()
	if _, err := c.Get(ctx, "https://example.com/x.png"); err == nil {
		t.Error("foreign host accepted")
	}
	for _, r := range s.Requests {
		if strings.Contains(r, "protocol_version=48") && strings.Contains(r, "engine_version=5.17.0") && strings.Contains(r, "type=mod&type=game&type=txp") {
			return
		}
	}
	t.Errorf("updates query not as the client sends it: %v", s.Requests)
}

func TestRetryAndRateLimit(t *testing.T) {
	s, c := newClient(t)
	s.Tags = []contentdb.Tag{}
	s.Fail["/api/tags/"] = 503
	ctx := context.Background()
	start := time.Now()
	if _, err := c.Tags(ctx); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if c.Requests != 2 {
		t.Errorf("requests = %d", c.Requests)
	}
	// Three more calls at 2 req/s must take at least a second in total.
	for i := 0; i < 3; i++ {
		c.Tags(ctx)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("rate limit not applied: %v for %d requests", elapsed, c.Requests)
	}
	s.Fail["/api/tags/"] = 404
	if _, err := c.Tags(ctx); !contentdb.IsNotFound(err) {
		t.Errorf("404 should not retry: %v (requests %d)", err, c.Requests)
	}
	if contentdb.New("", "ua", 50).Base != contentdb.DefaultURL {
		t.Error("default base")
	}
}
