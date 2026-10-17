package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/culmat/luanthetti/internal/app"
	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/version"
)

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/meta", s.meta)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/tags", s.tags)
	mux.HandleFunc("GET /api/packages/{author}/{name}", s.info)
	mux.HandleFunc("GET /api/installations", s.installations)
	mux.HandleFunc("POST /api/installations", s.addInstallation)
	mux.HandleFunc("DELETE /api/installations/{name}", s.removeInstallation)
	mux.HandleFunc("GET /api/detect", s.detect)
	mux.HandleFunc("GET /api/installed", s.installed)
	mux.HandleFunc("GET /api/updates", s.updates)
	mux.HandleFunc("POST /api/plan", s.plan)
	mux.HandleFunc("POST /api/jobs", s.submit)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.cancelJob)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/thumb", s.thumb)
}

// MetaResponse is GET /api/meta.
type MetaResponse struct {
	Version       string         `json:"version"`
	Offline       bool           `json:"offline"`
	Snapshot      *depgraph.Meta `json:"snapshot,omitempty"`
	SnapshotAge   float64        `json:"snapshot_age_days"`
	SnapshotStale bool           `json:"snapshot_stale"`
	SnapshotError string         `json:"snapshot_error,omitempty"`
	Installations int            `json:"installations"`
	ContentDB     string         `json:"contentdb"`
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	resp := MetaResponse{Version: version.Version(), Offline: s.App.Offline, ContentDB: s.App.CDB.Base}
	if m, err := s.App.SnapshotMeta(r.Context()); err != nil {
		resp.SnapshotError = err.Error()
	} else {
		resp.Snapshot = &m
		age, stale := s.App.Snap.Stale(m, time.Now())
		resp.SnapshotAge, resp.SnapshotStale = age.Hours()/24, stale
	}
	insts, _ := s.App.Store.Installations()
	resp.Installations = len(insts)
	writeJSON(w, http.StatusOK, resp)
}

func list(r *http.Request, key string) []string {
	var out []string
	for _, v := range r.URL.Query()[key] {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sq := app.SearchQuery{Q: q.Get("q"), Type: q.Get("type"), Tags: list(r, "tag"), DevStates: list(r, "dev_state"),
		CompatibleWith: list(r, "compatible_with"), Sort: q.Get("sort"), Thumbnails: q.Get("thumbnails") != "0"}
	sq.MinDownloads, _ = strconv.ParseInt(q.Get("min_downloads"), 10, 64)
	sq.MinScore, _ = strconv.ParseFloat(q.Get("min_score"), 64)
	sq.Limit, _ = strconv.Atoi(q.Get("limit"))
	if days, _ := strconv.Atoi(q.Get("updated_within_days")); days > 0 {
		sq.UpdatedWithin = time.Duration(days) * 24 * time.Hour
	}
	rep, err := s.App.Search(r.Context(), sq)
	respond(w, rep, err)
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.App.Tags(r.Context())
	respond(w, tags, err)
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	rep, err := s.App.Info(r.Context(), r.PathValue("author")+"/"+r.PathValue("name"))
	respond(w, rep, err)
}

func (s *Server) installations(w http.ResponseWriter, r *http.Request) {
	views, err := s.App.ListInstalls(r.Context())
	respond(w, views, err)
}

func (s *Server) addInstallation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path          string `json:"path"`
		Name          string `json:"name"`
		EngineVersion string `json:"engine_version"`
		LinkMode      string `json:"link_mode"`
		Create        bool   `json:"create"`
	}
	if !decode(w, r, &req) {
		return
	}
	inst, err := s.App.AddInstall(r.Context(), app.AddInstallRequest{Path: req.Path, Name: req.Name,
		EngineVersion: req.EngineVersion, LinkMode: req.LinkMode, Create: req.Create})
	respond(w, inst, err)
}

func (s *Server) removeInstallation(w http.ResponseWriter, r *http.Request) {
	rep, err := s.App.RemoveInstall(r.Context(), r.PathValue("name"), r.URL.Query().Get("unlink") == "1")
	respond(w, rep, err)
}

func (s *Server) detect(w http.ResponseWriter, r *http.Request) {
	rep, err := s.App.DetectInstalls(r.Context(), false)
	respond(w, rep, err)
}

func (s *Server) installed(w http.ResponseWriter, r *http.Request) {
	rep, err := s.App.List(r.Context(), app.ListRequest{Into: list(r, "installation"), Library: r.URL.Query().Get("library") == "1"})
	respond(w, rep, err)
}

func (s *Server) updates(w http.ResponseWriter, r *http.Request) {
	rep, err := s.App.Update(r.Context(), app.UpdateRequest{Check: true, Into: list(r, "installation")})
	respond(w, rep, err)
}

// JobRequest is POST /api/jobs and POST /api/plan.
type JobRequest struct {
	Kind    string   `json:"kind"` // install | remove | update | adopt | refresh | repair
	Specs   []string `json:"specs"`
	Into    []string `json:"into"`
	Force   bool     `json:"force"`
	NoDeps  bool     `json:"no_deps"`
	Exclude []string `json:"exclude"`
}

// plan is a dry run of an install, shown before the user confirms.
func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if !decode(w, r, &req) {
		return
	}
	rep, err := s.App.Install(r.Context(), app.InstallRequest{Specs: req.Specs, Into: req.Into, Force: req.Force, NoDeps: req.NoDeps, DryRun: true})
	respond(w, rep, err)
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if !decode(w, r, &req) {
		return
	}
	run, title, err := s.jobFunc(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	j, err := s.Jobs.Submit(req.Kind, title, run)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "busy", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": j.ID})
}

func (s *Server) jobFunc(req JobRequest) (func(ctx context.Context, logf func(string, ...any)) (any, error), string, error) {
	a := s.App
	title := req.Kind + " " + strings.Join(req.Specs, ", ")
	switch req.Kind {
	case "install":
		if len(req.Specs) == 0 {
			return nil, "", errors.New("specs required")
		}
		return func(ctx context.Context, _ func(string, ...any)) (any, error) {
			return a.Install(ctx, app.InstallRequest{Specs: req.Specs, Into: req.Into, Force: req.Force, NoDeps: req.NoDeps})
		}, title, nil
	case "remove":
		if len(req.Specs) == 0 {
			return nil, "", errors.New("specs required")
		}
		return func(ctx context.Context, _ func(string, ...any)) (any, error) {
			return a.Remove(ctx, app.RemoveRequest{Specs: req.Specs, Into: req.Into, Force: req.Force})
		}, title, nil
	case "update":
		return func(ctx context.Context, _ func(string, ...any)) (any, error) {
			return a.Update(ctx, app.UpdateRequest{Specs: req.Specs, Into: req.Into, Exclude: req.Exclude, NoDeps: req.NoDeps})
		}, strings.TrimSpace("update " + strings.Join(req.Specs, ", ")), nil
	case "adopt":
		inst := ""
		if len(req.Into) == 1 {
			inst = req.Into[0]
		}
		return func(ctx context.Context, _ func(string, ...any)) (any, error) {
			return a.Adopt(ctx, app.AdoptRequest{Installation: inst, Specs: req.Specs, Force: req.Force})
		}, "adopt " + inst, nil
	case "refresh":
		return func(ctx context.Context, _ func(string, ...any)) (any, error) {
			rep, err := a.Refresh(ctx)
			if err == nil {
				s.Jobs.Publish("snapshot", rep.Meta)
			}
			return rep, err
		}, "refresh", nil
	case "repair":
		return func(ctx context.Context, _ func(string, ...any)) (any, error) {
			return a.Repair(ctx, app.RepairRequest{Into: req.Into, Relink: true, Readopt: true})
		}, "repair", nil
	}
	return nil, "", fmt.Errorf("unknown job kind %q", req.Kind)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Jobs.List())
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "no such job")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if !s.Jobs.Cancel(r.PathValue("id")) {
		writeErr(w, http.StatusConflict, "not_running", "the job is not queued or running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

// events streams job changes and log lines as server-sent events.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "no_stream", "streaming unsupported")
		return
	}
	last, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	ch, unsub := s.Jobs.Subscribe(last)
	defer unsub()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case ev := <-ch:
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, ev.Data)
			flusher.Flush()
		}
	}
}

func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "url required")
		return
	}
	path, ct, err := s.Thumbs.Get(r.Context(), url)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		var nf *depgraph.NotFoundError
		switch {
		case errors.As(err, &nf):
			writeErr(w, http.StatusNotFound, "not_found", err.Error())
		default:
			writeErr(w, http.StatusUnprocessableEntity, "failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, v)
}
