package controlplane

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type HTTPServer struct {
	Service   *Service
	StaticDir string
}

func (h *HTTPServer) Handler() http.Handler { return http.HandlerFunc(h.serveHTTP) }
func (h *HTTPServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		h.api(w, r)
		return
	}
	if h.StaticDir != "" {
		index := filepath.Join(h.StaticDir, "index.html")
		if _, e := os.Stat(index); e == nil {
			http.ServeFile(w, r, index)
			return
		}
	}
	http.NotFound(w, r)
}
func (h *HTTPServer) api(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if p == "agents" {
		if r.Method == http.MethodGet {
			h.listAgents(w, r)
			return
		}
		if r.Method == http.MethodPost {
			h.createAgent(w, r)
			return
		}
	}
	if p == "runs" && r.Method == http.MethodGet {
		h.listRuns(w, r, "")
		return
	}
	if len(parts) == 3 && parts[0] == "runs" && parts[2] == "events" && r.Method == http.MethodGet {
		h.events(w, r, parts[1])
		return
	}
	if len(parts) >= 2 && parts[0] == "agents" {
		id := parts[1]
		if len(parts) == 2 && r.Method == http.MethodGet {
			h.getAgent(w, r, id)
			return
		}
		if len(parts) == 2 && strings.Contains(id, ":") {
			x := strings.SplitN(id, ":", 2)
			h.action(w, r, x[0], x[1])
			return
		}
		if len(parts) == 3 && parts[2] == "runs" {
			if r.Method == http.MethodGet {
				h.listRuns(w, r, id)
				return
			}
			if r.Method == http.MethodPost {
				h.startRun(w, r, id)
				return
			}
		}
		if len(parts) >= 3 && parts[2] == "workspace" {
			if len(parts) == 3 && r.Method == http.MethodGet {
				h.workspace(w, r, id)
				return
			}
			if len(parts) == 4 && parts[3] == "file" && r.Method == http.MethodGet {
				h.workspaceFile(w, r, id)
				return
			}
		}
	}
	writeError(w, http.StatusNotFound, "not_found", "route not found")
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}
func (h *HTTPServer) listAgents(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.Store.List(r.Context())
	reply(w, e, v)
}
func (h *HTTPServer) getAgent(w http.ResponseWriter, r *http.Request, id string) {
	v, e := h.Service.Store.Get(r.Context(), id)
	reply(w, e, v)
}
func (h *HTTPServer) createAgent(w http.ResponseWriter, r *http.Request) {
	var in CreateAgentInput
	if e := decode(r, &in); e != nil {
		writeError(w, 400, "invalid_json", e.Error())
		return
	}
	v, e := h.Service.Create(r.Context(), in)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (h *HTTPServer) action(w http.ResponseWriter, r *http.Request, id, act string) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "")
		return
	}
	var v AgentSpec
	var e error
	switch act {
	case "start":
		v, e = h.Service.StartAgent(r.Context(), id)
	case "stop":
		v, e = h.Service.StopAgent(r.Context(), id)
	case "delete":
		e = h.Service.DeleteAgent(r.Context(), id)
		if e == nil {
			w.WriteHeader(204)
			return
		}
	default:
		writeError(w, 404, "not_found", "unknown action")
		return
	}
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *HTTPServer) listRuns(w http.ResponseWriter, r *http.Request, id string) {
	v, e := h.Service.Store.ListRuns(r.Context(), id)
	reply(w, e, v)
}
func (h *HTTPServer) startRun(w http.ResponseWriter, r *http.Request, id string) {
	var in struct {
		Prompt string `json:"prompt"`
	}
	if e := decode(r, &in); e != nil {
		writeError(w, 400, "invalid_json", e.Error())
		return
	}
	v, e := h.Service.StartRun(r.Context(), id, in.Prompt)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 202, v)
}
func (h *HTTPServer) workspace(w http.ResponseWriter, r *http.Request, id string) {
	a, e := h.Service.Store.Get(r.Context(), id)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := h.Service.Docker.ListWorkspace(r.Context(), a, r.URL.Query().Get("path"))
	reply(w, e, v)
}
func (h *HTTPServer) workspaceFile(w http.ResponseWriter, r *http.Request, id string) {
	a, e := h.Service.Store.Get(r.Context(), id)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	v, e := h.Service.Docker.ReadWorkspaceFile(r.Context(), a, r.URL.Query().Get("path"))
	reply(w, e, map[string]string{"content": v})
}
func (h *HTTPServer) events(w http.ResponseWriter, r *http.Request, id string) {
	last, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming_unsupported", "")
		return
	}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	ctx := r.Context()
	for {
		events, e := h.Service.Events.After(ctx, id, last)
		if e != nil {
			return
		}
		for _, ev := range events {
			raw, _ := json.Marshal(ev.Data)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, raw)
			f.Flush()
			last = ev.ID
			if ev.Type == "done" || ev.Type == "error" {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			f.Flush()
		}
	}
}
func reply(w http.ResponseWriter, e error, v any) {
	if e != nil {
		writeServiceError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func writeServiceError(w http.ResponseWriter, e error) {
	if errors.Is(e, sql.ErrNoRows) {
		writeError(w, 404, "not_found", "resource not found")
		return
	}
	if IsConflict(e) || strings.Contains(e.Error(), "active run") {
		writeError(w, 409, "conflict", e.Error())
		return
	}
	writeError(w, 400, "request_failed", e.Error())
}
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSONStatus(w, status, map[string]string{"code": code, "message": msg})
}
func writeJSON(w http.ResponseWriter, status int, v any) { writeJSONStatus(w, status, v) }
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
