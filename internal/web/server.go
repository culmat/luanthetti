// Package web serves Luanthetti's browser UI on localhost: a JSON API over the app layer,
// server-sent events for job progress, a thumbnail proxy, and the embedded static page.
//
// Security: it binds to 127.0.0.1 only; every /api/ request needs the per-run token that is
// part of the URL the command prints (header X-Luanthetti-Token, or ?t= for EventSource);
// the Host header must be the loopback address (DNS rebinding); cross-site requests are
// refused; no CORS headers are ever sent.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/culmat/luanthetti/internal/app"
	"github.com/culmat/luanthetti/internal/jobs"
	"github.com/culmat/luanthetti/internal/thumbs"
)

//go:embed static
var static embed.FS

// Server is the UI server.
type Server struct {
	App    *app.App
	Jobs   *jobs.Manager
	Thumbs *thumbs.Cache
	Token  string
	addr   string
	srv    *http.Server
}

// New prepares a server for a; it routes the app's logs into the running job.
func New(ctx context.Context, a *app.App) *Server {
	s := &Server{App: a, Jobs: jobs.New(ctx), Token: newToken()}
	s.Thumbs = &thumbs.Cache{Dir: a.Dirs.ThumbsDir(), MaxBytes: 100 << 20, Fetch: a.CDB, Offline: a.Offline}
	prev := a.Log
	a.SetLog(func(format string, args ...any) {
		prev(format, args...)
		s.Jobs.Logf(nil, format, args...)
	})
	return s
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Listen binds 127.0.0.1:port (0 = any free port) and returns the URL to open.
func (s *Server) Listen(port int) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil && port != 0 {
		ln, err = net.Listen("tcp", "127.0.0.1:0") // the preferred port is taken
	}
	if err != nil {
		return nil, "", err
	}
	s.addr = ln.Addr().String()
	return ln, fmt.Sprintf("http://%s/#t=%s", s.addr, s.Token), nil
}

// Serve runs until ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.srv.Shutdown(shutdown)
	}()
	err := s.srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler is the full HTTP handler (exported for tests).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	files := http.FileServerFS(sub)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		files.ServeHTTP(w, r)
	}))
	api := http.NewServeMux()
	s.routes(api)
	mux.Handle("/api/", s.guard(api))
	return s.headers(mux)
}

func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		if !s.hostOK(r.Host) {
			http.Error(w, "wrong host", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostOK accepts only the loopback address we listen on (and localhost with that port).
func (s *Server) hostOK(host string) bool {
	if s.addr == "" {
		return true // tests using Handler() directly set no address
	}
	_, port, _ := net.SplitHostPort(s.addr)
	return host == s.addr || host == "localhost:"+port
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Luanthetti-Token")
		// EventSource and <img> cannot send headers; these two read-only endpoints take ?t=.
		if token == "" && (r.URL.Path == "/api/events" || r.URL.Path == "/api/thumb") {
			token = r.URL.Query().Get("t")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing or wrong token; open the URL printed by `luanthetti ui`")
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeErr(w, http.StatusForbidden, "cross_site", "cross-site request refused")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && s.addr != "" && origin != "http://"+s.addr {
			writeErr(w, http.StatusForbidden, "cross_site", "foreign origin refused")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeErr(w, http.StatusUnsupportedMediaType, "bad_request", "send application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
