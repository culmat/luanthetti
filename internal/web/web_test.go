package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/culmat/luanthetti/internal/app"
	"github.com/culmat/luanthetti/internal/contentdb/fake"
)

func setup(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	srv := fake.New()
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "pipeworks")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "init.lua"), nil, 0o644)
	z, _ := fake.ZipDir(dir, "pipeworks")
	for _, p := range []struct{ author, name string }{{"mt-mods", "pipeworks"}, {"other", "xcompat"}} {
		pk := &fake.Package{}
		pk.Author, pk.Name, pk.Type, pk.Title = p.author, p.name, "mod", p.name
		pk.Thumbnail = srv.URL + "/thumbnails/1/" + p.name + ".png"
		srv.Add(pk)
		srv.AddRelease(p.author, p.name, 500, "v1", z)
	}
	a, err := app.New(app.Options{Home: t.TempDir(), ContentDBURL: srv.URL, SnapshotFile: "../depgraph/testdata/mini.sqlite",
		HTTPClient: srv.Client(), ContentDBRPS: 1000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := New(ctx, a)
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	inst := t.TempDir()
	g := filepath.Join(inst, "games", "minetest_game")
	os.MkdirAll(g, 0o755)
	os.WriteFile(filepath.Join(g, "game.conf"), []byte("author = Luanti\nrelease = 1\n"), 0o644)
	os.MkdirAll(filepath.Join(inst, "mods"), 0o755)
	return s, hs, inst
}

func call(t *testing.T, s *Server, hs *httptest.Server, method, path string, body any, mutate ...func(*http.Request)) (int, map[string]any, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, hs.URL+path, rd)
	req.Header.Set("X-Luanthetti-Token", s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mutate {
		m(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out, raw
}

func TestSecurity(t *testing.T) {
	s, hs, _ := setup(t)
	if code, _, _ := call(t, s, hs, "GET", "/api/meta", nil, func(r *http.Request) { r.Header.Del("X-Luanthetti-Token") }); code != 401 {
		t.Errorf("no token: %d", code)
	}
	if code, _, _ := call(t, s, hs, "GET", "/api/meta", nil, func(r *http.Request) { r.Header.Set("X-Luanthetti-Token", "wrong") }); code != 401 {
		t.Errorf("wrong token: %d", code)
	}
	if code, _, _ := call(t, s, hs, "GET", "/api/meta", nil, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }); code != 403 {
		t.Errorf("cross-site: %d", code)
	}
	if code, _, _ := call(t, s, hs, "POST", "/api/jobs", nil, func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(`{"kind":"refresh"}`))
		r.Header.Set("Content-Type", "text/plain")
	}); code != 415 {
		t.Errorf("form post: %d", code)
	}
	// Host check once the server knows its address.
	s.addr = "127.0.0.1:1"
	if code, _, _ := call(t, s, hs, "GET", "/api/meta", nil); code != http.StatusMisdirectedRequest {
		t.Errorf("rebinding host: %d", code)
	}
	s.addr = ""
	resp, _ := http.Get(hs.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "<title>Luanthetti</title>") {
		t.Errorf("index: %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Errorf("headers %v", resp.Header)
	}
	for _, f := range []string{"/app.js", "/style.css", "/icon.svg"} {
		if r, _ := http.Get(hs.URL + f); r.StatusCode != 200 {
			t.Errorf("%s: %d", f, r.StatusCode)
		}
	}
}

func TestAPIFlow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based flow")
	}
	s, hs, inst := setup(t)
	code, meta, _ := call(t, s, hs, "GET", "/api/meta", nil)
	if code != 200 || meta["snapshot"] == nil {
		t.Fatalf("%d %v", code, meta)
	}
	code, out, raw := call(t, s, hs, "POST", "/api/installations", map[string]any{"path": inst, "name": "desk", "engine_version": "5.17.0"})
	if code != 200 || out["name"] != "desk" {
		t.Fatalf("%d %s", code, raw)
	}
	code, out, raw = call(t, s, hs, "GET", "/api/search?q=xcompat&type=mod", nil)
	hits, _ := out["hits"].([]any)
	if code != 200 || len(hits) != 2 {
		t.Fatalf("%d %s", code, raw)
	}
	first := hits[0].(map[string]any)
	if first["package"] != "other/xcompat" || first["compat"].(map[string]any)["Luanti/minetest_game"] != "ok" || !strings.Contains(first["thumbnail"].(string), "/thumbnails/") {
		t.Errorf("%v", first)
	}
	// Thumbnail proxy with ?t= (as <img> sends it).
	resp, _ := http.Get(hs.URL + "/api/thumb?t=" + s.Token + "&url=" + first["thumbnail"].(string))
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("thumb %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()
	if r, _ := http.Get(hs.URL + "/api/thumb?t=" + s.Token + "&url=https://example.com/x.png"); r.StatusCode != 204 {
		t.Errorf("foreign thumb: %d", r.StatusCode)
	}
	code, out, raw = call(t, s, hs, "POST", "/api/plan", map[string]any{"kind": "install", "specs": []string{"mt-mods/pipeworks"}})
	if code != 200 || out["dry_run"] != true {
		t.Fatalf("%d %s", code, raw)
	}

	// Subscribe to events, submit the install job, wait for "done".
	evReq, _ := http.NewRequest("GET", hs.URL+"/api/events?t="+s.Token, nil)
	evResp, err := http.DefaultClient.Do(evReq)
	if err != nil {
		t.Fatal(err)
	}
	defer evResp.Body.Close()
	code, out, raw = call(t, s, hs, "POST", "/api/jobs", map[string]any{"kind": "install", "specs": []string{"mt-mods/pipeworks"}})
	if code != 202 {
		t.Fatalf("%d %s", code, raw)
	}
	id := out["id"].(string)
	done := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(evResp.Body)
		var event string
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") && event == "job" && strings.Contains(line, id) {
				var j map[string]any
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &j)
				if st := j["state"].(string); st == "done" || st == "failed" {
					done <- st + " " + line
					return
				}
			}
		}
	}()
	select {
	case got := <-done:
		if !strings.HasPrefix(got, "done") {
			t.Fatalf("job: %s", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no done event")
	}
	code, out, _ = call(t, s, hs, "GET", "/api/jobs/"+id, nil)
	if code != 200 || out["state"] != "done" || len(out["log"].([]any)) == 0 {
		t.Errorf("%d %v", code, out)
	}
	code, out, raw = call(t, s, hs, "GET", "/api/installed", nil)
	if rows, _ := out["rows"].([]any); code != 200 || len(rows) != 2 {
		t.Errorf("%d %s", code, raw)
	}
	code, _, raw = call(t, s, hs, "GET", "/api/packages/mt-mods/pipeworks", nil)
	if code != 200 || !strings.Contains(string(raw), `"installed":[{`) {
		t.Errorf("%d %s", code, raw)
	}
	if code, _, _ := call(t, s, hs, "GET", "/api/packages/no/such", nil); code != 404 {
		t.Errorf("404: %d", code)
	}
	code, _, _ = call(t, s, hs, "GET", "/api/updates", nil)
	if code != 200 {
		t.Errorf("updates %d", code)
	}
	if code, _, _ := call(t, s, hs, "POST", "/api/jobs", map[string]any{"kind": "explode"}); code != 400 {
		t.Errorf("bad kind %d", code)
	}
	if code, _, _ := call(t, s, hs, "POST", "/api/jobs", map[string]any{"kind": "install", "bogus": 1}); code != 400 {
		t.Errorf("unknown field %d", code)
	}
}
