// Package server implements the infowall HTTP server: REST API, SSE endpoint, and static frontend serving.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/feed"
	"github.com/infowall/infowall/internal/parser"
	"github.com/infowall/infowall/internal/store"
)

// Config holds server options.
type Config struct {
	Addr   string
	DBPath string
	Dev    bool   // true → proxy frontend to Vite dev server on :5173
	APIKey string // optional; if set, requests must carry Authorization: Bearer <key> or ?key=<key>
	DistFS fs.FS  // embedded production frontend (ignored in Dev mode)
}

// Server wires together store, hub, and HTTP routes.
type Server struct {
	cfg   Config
	store *store.Store
	hub   *feed.Hub
	mux   *http.ServeMux
}

// New constructs a Server and opens the SQLite store.
func New(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":8899"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "infowall.db"
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:   cfg,
		store: st,
		hub:   feed.NewHub(),
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

// Close releases resources held by the server (e.g. the database).
func (s *Server) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Server) routes() {
	api := chain(s.logRequest, s.auth)

	s.mux.HandleFunc("GET /api/health", api(s.handleHealth))
	s.mux.HandleFunc("GET /api/items", api(s.handleListItems))
	s.mux.HandleFunc("GET /api/items/{id}", api(s.handleGetItem))
	s.mux.HandleFunc("POST /api/items", api(s.handleCreateItem))
	s.mux.HandleFunc("POST /api/items/{id}/pin", api(s.handlePinItem))
	s.mux.HandleFunc("DELETE /api/items/{id}", api(s.handleDeleteItem))

	// SSE endpoint: same auth as API (accepts ?key= for EventSource), also logged.
	s.mux.HandleFunc("GET /events", s.logRequest(s.auth(s.handleEvents)))

	if s.cfg.Dev {
		proxy := devProxy("http://localhost:5173")
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/events" {
				http.NotFound(w, r)
				return
			}
			proxy.ServeHTTP(w, r)
		})
		log.Printf("dev mode: proxying / to Vite at http://localhost:5173")
	} else if s.cfg.DistFS != nil {
		sub, err := fs.Sub(s.cfg.DistFS, "dist")
		if err != nil {
			sub = s.cfg.DistFS
		}
		fileServer := spaFileServer(http.FS(sub))
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/events" {
				http.NotFound(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html><body style="font-family:system-ui;max-width:640px;margin:4rem auto;padding:0 1rem;">
<h1>infowall</h1>
<p>Frontend not built. Run <code>make web</code> or pass <code>--dev</code> (with Vite running on :5173).</p>
</body></html>`)
		})
	}
}

// ListenAndServe starts the HTTP server. It blocks until the server shuts down.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE connections stay open indefinitely
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("infowall listening on %s (dev=%v, db=%s)", s.cfg.Addr, s.cfg.Dev, s.cfg.DBPath)
	return srv.ListenAndServe()
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().UTC()})
}

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := intParam(q.Get("limit"), 50, 1, 200)
	offset := intParam(q.Get("offset"), 0, 0, 1_000_000)
	typeFilter := strings.TrimSpace(q.Get("type"))
	if typeFilter == "" {
		typeFilter = strings.TrimSpace(q.Get("topic"))
	}

	items, err := s.store.List(r.Context(), limit, offset, typeFilter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if q.Get("raw") != "1" {
		for _, it := range items {
			it.Raw = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleGetItem returns a single item by id. The raw markdown source is included
// only when ?raw=1 is passed, mirroring handleListItems.
func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	it, err := s.store.Get(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	if r.URL.Query().Get("raw") != "1" {
		it.Raw = ""
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	raw, err := readBodySmart(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	it, err := parser.Parse(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.Insert(r.Context(), it); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.broadcast("item.new", it)
	it.Raw = ""
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) handlePinItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Pinned *bool `json:"pinned"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	pinned := true // default when called with no body
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
			return
		}
		if req.Pinned != nil {
			pinned = *req.Pinned
		}
	} else if v := r.URL.Query().Get("pinned"); v != "" {
		pinned = v == "1" || strings.EqualFold(v, "true")
	}
	if err := s.store.SetPinned(r.Context(), id, pinned); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	s.broadcast("item.pin", map[string]any{"id": id, "pinned": pinned})
	if it, err := s.store.Get(r.Context(), id); err == nil {
		it.Raw = ""
		writeJSON(w, http.StatusOK, it)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "pinned": pinned})
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(r.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	s.broadcast("item.delete", map[string]any{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Tell clients to reconnect after 3s on disconnect, then send a comment ping so
	// intermediaries see data on the wire immediately.
	fmt.Fprint(w, "retry: 3000\n\n")
	fmt.Fprint(w, ": ping\n\n")
	flusher.Flush()

	ch, cancel := s.hub.Subscribe()
	defer cancel()

	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()

	// Notify when client goes away.
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			// ev.Data is already pre-marshaled JSON from broadcast().
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data)
			flusher.Flush()
		}
	}
}

// --- helpers ---

func (s *Server) broadcast(name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.hub.Publish(feed.Event{Name: name, Data: data})
}

func (s *Server) logRequest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next(lw, r)
		log.Printf("%s %s → %d (%s)", r.Method, r.URL.RequestURI(), lw.status, time.Since(start).Round(time.Millisecond))
	}
}

// auth is middleware for API and SSE endpoints, requiring a Bearer token or ?key=
// query parameter when Config.APIKey is set.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkKey(r) {
			writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		next(w, r)
	}
}

func (s *Server) checkKey(r *http.Request) bool {
	if s.cfg.APIKey == "" {
		return true
	}
	token := ""
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("key"))
	}
	return token == s.cfg.APIKey
}

type responseWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wrote {
		return
	}
	rw.status = code
	rw.wrote = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wrote {
		rw.wrote = true
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher so SSE endpoints work through the logging middleware.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func chain(mws ...func(http.HandlerFunc) http.HandlerFunc) func(http.HandlerFunc) http.HandlerFunc {
	return func(final http.HandlerFunc) http.HandlerFunc {
		h := final
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON error: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func intParam(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < min {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// readBodySmart handles both raw markdown bodies (text/markdown, text/plain,
// application/octet-stream) and JSON bodies of shape {"raw": "..."}. Body is capped at 4MB.
func readBodySmart(r *http.Request) ([]byte, error) {
	ct := r.Header.Get("Content-Type")
	// Strip parameters (e.g. "text/markdown; charset=utf-8").
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	lr := io.LimitReader(r.Body, 4<<20) // 4MB cap
	if strings.EqualFold(ct, "application/json") {
		var req struct {
			Raw string `json:"raw"`
		}
		if err := json.NewDecoder(lr).Decode(&req); err != nil {
			return nil, fmt.Errorf("invalid JSON body: %w", err)
		}
		if req.Raw == "" {
			return nil, fmt.Errorf("JSON body missing 'raw' field")
		}
		return []byte(req.Raw), nil
	}
	raw, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	return raw, nil
}

// devProxy returns a reverse proxy that forwards non-API requests to the Vite dev server.
func devProxy(target string) *httputil.ReverseProxy {
	u, err := url.Parse(target)
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `<!doctype html><html><body style="font-family:system-ui;max-width:640px;margin:4rem auto;padding:0 1rem;">
<h2>Frontend dev server not reachable</h2>
<p>Expected Vite at <code>%s</code>. Run <code>cd web &amp;&amp; npm run dev</code> in another terminal.</p>
<p><small>%v</small></p></body></html>`, target, err)
	}
	return proxy
}

// spaFileServer serves static files and falls back to index.html for SPA routes (any path
// that does not correspond to a real file).
func spaFileServer(root http.FileSystem) http.Handler {
	fs := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			fs.ServeHTTP(w, r)
			return
		}
		// Clean path to prevent traversal; http.Dir.Open rejects ".." anyway.
		clean := strings.TrimPrefix(path, "/")
		f, err := root.Open(clean)
		if err != nil {
			// SPA fallback: serve index.html at root.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fs.ServeHTTP(w, r2)
			return
		}
		f.Close()
		fs.ServeHTTP(w, r)
	})
}
