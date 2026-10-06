// Package server implements the infowall HTTP server: REST API, SSE endpoint, and static frontend serving.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
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
	"sync"
	"time"

	"github.com/infowall/infowall/internal/feed"
	"github.com/infowall/infowall/internal/feishuingest"
	"github.com/infowall/infowall/internal/feishusync"
	"github.com/infowall/infowall/internal/integration"
	"github.com/infowall/infowall/internal/parser"
	"github.com/infowall/infowall/internal/store"
)

// Config holds server options.
type Config struct {
	Addr                     string
	DBPath                   string
	Dev                      bool   // true → proxy frontend to Vite dev server on :5173
	APIKey                   string // optional; if set, mutations must carry Authorization: Bearer <key>
	DistFS                   fs.FS  // embedded production frontend (ignored in Dev mode)
	DefaultView              string // "infowall" or "workbench"; used when the browser URL has no valid hash route
	DisableBackgroundWorkers bool   // serve stored data without automatic Feishu sync or activity scans
	ReadOnly                 bool   // reject API mutations for a mirror instance
	InstanceRole             string // primary, mirror, or development; exposed for agent discovery
	BuildVersion             string // injected by the CLI build
	BuildCommit              string // injected by the CLI build
	// Integrations selects the process integration composition. Nil preserves
	// the legacy built-in Feishu, command enrichment, analyzer, and document
	// sync behavior. A non-nil empty value runs the core server without any
	// integration adapters; otherwise only the supplied components are used.
	Integrations *integration.Components

	// Legacy integration settings are honored only when Integrations is nil.
	// LarkRunner is injectable for tests. Production uses lark-cli from PATH.
	LarkRunner         feishusync.CommandRunner
	IngestionCollector feishuingest.Collector
	IngestionAnalyzer  feishuingest.Analyzer
	CodexPath          string
	CodexCWD           string
	CodexTimeout       time.Duration
	ClaudePath         string
	ClaudePresetsPath  string
	ClaudePreset       string
	ClaudeTimeout      time.Duration
}

// Server wires together store, hub, and HTTP routes.
type Server struct {
	cfg   Config
	store *store.Store
	hub   *feed.Hub
	mux   *http.ServeMux

	integrations integration.Components
	feishuClient *feishusync.Client
	syncWorker   *feishusync.Worker
	ingestWorker *feishuingest.Worker
	syncCancel   context.CancelFunc
	syncWG       sync.WaitGroup
}

const (
	defaultViewSettingKey       = "default_view"
	defaultIngestionLarkTimeout = 90 * time.Second
)

func normalizeDefaultView(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "infowall" && value != "workbench" {
		return "", fmt.Errorf("invalid default view %q (want infowall or workbench)", value)
	}
	return value, nil
}

// New constructs a Server and opens the SQLite store.
func New(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":8899"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "infowall.db"
	}
	if cfg.DefaultView == "" {
		cfg.DefaultView = "workbench"
	}
	var err error
	cfg.DefaultView, err = normalizeDefaultView(cfg.DefaultView)
	if err != nil {
		return nil, err
	}
	if cfg.InstanceRole == "" {
		cfg.InstanceRole = "primary"
	}
	if cfg.InstanceRole != "primary" && cfg.InstanceRole != "mirror" && cfg.InstanceRole != "development" {
		return nil, fmt.Errorf("invalid instance role %q (want primary, mirror, or development)", cfg.InstanceRole)
	}
	resolved, err := resolveIntegrations(cfg)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", cfg.DBPath, err)
	}
	settingsContext := ctx
	if settingsContext == nil {
		settingsContext = context.Background()
	}
	persistedView, found, err := st.GetSetting(settingsContext, defaultViewSettingKey)
	if err != nil {
		st.Close()
		return nil, err
	}
	if found {
		cfg.DefaultView, err = normalizeDefaultView(persistedView)
		if err != nil {
			st.Close()
			return nil, fmt.Errorf("persisted frontend config: %w", err)
		}
	} else if err := st.SetSetting(settingsContext, defaultViewSettingKey, cfg.DefaultView); err != nil {
		st.Close()
		return nil, err
	}
	s := &Server{
		cfg:          cfg,
		store:        st,
		hub:          feed.NewHub(),
		mux:          http.NewServeMux(),
		integrations: resolved.components,
	}
	s.wireIntegrations(resolved)
	s.startIntegrationWorkers(ctx)
	s.routes()
	return s, nil
}

// Close releases resources held by the server (e.g. the database).
func (s *Server) Close() error {
	if s.syncCancel != nil {
		s.syncCancel()
		s.syncWG.Wait()
	}
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Server) routes() {
	read := s.logRequest
	write := chain(s.logRequest, s.requireWriteAuth)

	s.mux.HandleFunc("GET /api/health", read(s.handleHealth))
	s.mux.HandleFunc("GET /api/capabilities", read(s.handleCapabilities))
	s.mux.HandleFunc("POST /api/auth/write-check", write(s.handleWriteAuthCheck))
	s.mux.HandleFunc("GET /api/config", read(s.handleFrontendConfig))
	s.mux.HandleFunc("PATCH /api/config", write(s.handleUpdateFrontendConfig))
	s.mux.HandleFunc("GET /api/items", read(s.handleListItems))
	s.mux.HandleFunc("GET /api/items/{id}", read(s.handleGetItem))
	s.mux.HandleFunc("POST /api/items", write(s.handleCreateItem))
	s.mux.HandleFunc("POST /api/items/{id}/pin", write(s.handlePinItem))
	s.mux.HandleFunc("DELETE /api/items/{id}", write(s.handleDeleteItem))
	s.mux.HandleFunc("GET /api/demands", read(s.handleListDemands))
	s.mux.HandleFunc("POST /api/demands", write(s.handleCreateDemand))
	s.mux.HandleFunc("POST /api/demands/import", write(s.handleImportDemands))
	s.mux.HandleFunc("GET /api/demands/{id}", read(s.handleGetDemand))
	s.mux.HandleFunc("PATCH /api/demands/{id}", write(s.handlePatchDemand))
	s.mux.HandleFunc("POST /api/demands/{id}/progress", write(s.handleAddDemandProgress))
	s.mux.HandleFunc("GET /api/projects", read(s.handleListProjects))
	s.mux.HandleFunc("POST /api/projects", write(s.handleCreateProject))
	s.mux.HandleFunc("GET /api/projects/{id}", read(s.handleGetProject))
	s.mux.HandleFunc("PATCH /api/projects/{id}", write(s.handlePatchProject))
	s.mux.HandleFunc("GET /api/integrations/feishu-doc", read(s.handleGetFeishuDoc))
	s.mux.HandleFunc("POST /api/integrations/feishu-doc", write(s.handleSetupFeishuDoc))
	s.mux.HandleFunc("DELETE /api/integrations/feishu-doc", write(s.handleDisableFeishuDoc))
	s.mux.HandleFunc("POST /api/integrations/feishu-doc/sync", write(s.handleSyncFeishuDoc))
	s.mux.HandleFunc("GET /api/integrations/feishu-chat", read(s.handleGetFeishuChat))
	s.mux.HandleFunc("PATCH /api/integrations/feishu-chat", write(s.handlePatchFeishuChat))
	s.mux.HandleFunc("POST /api/integrations/feishu-chat/scan", write(s.handleScanFeishuChat))
	s.mux.HandleFunc("GET /api/integrations/feishu-chat/runs", read(s.handleListFeishuChatRuns))
	s.mux.HandleFunc("POST /api/integrations/conversations/events", write(s.handlePutConversationHookEvent))
	// Unified aliases keep the original Feishu CLI/API contract compatible.
	s.mux.HandleFunc("GET /api/integrations/activity", read(s.handleGetFeishuChat))
	s.mux.HandleFunc("PATCH /api/integrations/activity", write(s.handlePatchFeishuChat))
	s.mux.HandleFunc("POST /api/integrations/activity/scan", write(s.handleScanFeishuChat))
	s.mux.HandleFunc("GET /api/integrations/activity/runs", read(s.handleListFeishuChatRuns))
	s.mux.HandleFunc("GET /api/demand-reviews", read(s.handleListDemandReviews))
	s.mux.HandleFunc("POST /api/demand-reviews/{id}/accept", write(s.handleAcceptDemandReview))
	s.mux.HandleFunc("POST /api/demand-reviews/{id}/dismiss", write(s.handleDismissDemandReview))

	// SSE carries the same read-only representation as the public GET API.
	s.mux.HandleFunc("GET /events", read(s.handleEvents))

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
	return s.ListenAndServeContext(context.Background())
}

// ListenAndServeContext starts the HTTP server and drains it when ctx is
// cancelled. Service managers can therefore stop the process without cutting
// off in-flight SQLite-backed requests.
func (s *Server) ListenAndServeContext(ctx context.Context) error {
	srv := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE connections stay open indefinitely
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("infowall listening on %s (dev=%v, db=%s, default_view=%s)", s.cfg.Addr, s.cfg.Dev, s.cfg.DBPath, s.cfg.DefaultView)
	result := make(chan error, 1)
	go func() { result <- srv.ListenAndServe() }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		if err := <-result; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"service":        "infowall",
		"status":         "ok",
		"version":        s.cfg.BuildVersion,
		"commit":         s.cfg.BuildCommit,
		"api_version":    "1",
		"instance_role":  s.cfg.InstanceRole,
		"read_only":      s.cfg.ReadOnly,
		"schema_version": store.SchemaVersion(),
		"ts":             time.Now().UTC(),
	})
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeAuth := "none"
	if s.cfg.APIKey != "" {
		writeAuth = "bearer"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":            "infowall",
		"api_version":        "1",
		"version":            s.cfg.BuildVersion,
		"commit":             s.cfg.BuildCommit,
		"instance_role":      s.cfg.InstanceRole,
		"read_only":          s.cfg.ReadOnly,
		"background_workers": !s.cfg.DisableBackgroundWorkers && (s.syncWorker != nil || s.ingestWorker != nil),
		"schema_version":     store.SchemaVersion(),
		"read_access":        "public",
		"write_auth":         writeAuth,
		"sse_access":         "public",
		"features": []string{
			"agent-spec-v12", "demand-apply", "structured-errors",
			"progress-links", "sse",
		},
	})
}

func (s *Server) handleWriteAuthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"write_authorized": true,
	})
}

func (s *Server) handleFrontendConfig(w http.ResponseWriter, r *http.Request) {
	defaultView, found, err := s.store.GetSetting(r.Context(), defaultViewSettingKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		defaultView = s.cfg.DefaultView
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"default_view": defaultView,
	})
}

func (s *Server) handleUpdateFrontendConfig(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DefaultView string `json:"default_view"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	defaultView, err := normalizeDefaultView(request.DefaultView)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetSetting(r.Context(), defaultViewSettingKey, defaultView); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	payload := map[string]string{"default_view": defaultView}
	s.broadcast("config.updated", payload)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := intParam(q.Get("limit"), 50, 1, 200)
	offset := intParam(q.Get("offset"), 0, 0, 1_000_000)
	typeFilter := strings.TrimSpace(q.Get("type"))
	if typeFilter == "" {
		typeFilter = strings.TrimSpace(q.Get("topic"))
	}
	query := strings.TrimSpace(q.Get("q"))
	pinnedOnly := false
	if v := strings.TrimSpace(q.Get("pinned")); v != "" {
		pinnedOnly = v == "1" || strings.EqualFold(v, "true")
	}

	var cursor *store.ListCursor
	if rawCursor := strings.TrimSpace(q.Get("cursor")); rawCursor != "" {
		decoded, err := decodeListCursor(rawCursor)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		cursor = decoded
	}

	page, err := s.store.ListPage(r.Context(), store.ListOptions{
		Limit:      limit,
		Offset:     offset,
		TypeFilter: typeFilter,
		Query:      query,
		PinnedOnly: pinnedOnly,
		After:      cursor,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if q.Get("raw") != "1" {
		for _, it := range page.Items {
			it.Raw = ""
		}
	}
	payload := map[string]any{
		"items":    page.Items,
		"has_more": page.HasMore,
	}
	if page.NextCursor != nil {
		payload["next_cursor"] = encodeListCursor(page.NextCursor)
	}
	writeJSON(w, http.StatusOK, payload)
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

type listCursorPayload struct {
	Pinned    bool   `json:"pinned"`
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

func encodeListCursor(cursor *store.ListCursor) string {
	payload := listCursorPayload{
		Pinned:    cursor.Pinned,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        cursor.ID,
	}
	b, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeListCursor(raw string) (*store.ListCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	var payload listCursorPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	if strings.TrimSpace(payload.ID) == "" || strings.TrimSpace(payload.CreatedAt) == "" {
		return nil, fmt.Errorf("invalid cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &store.ListCursor{
		Pinned:    payload.Pinned,
		CreatedAt: createdAt.UTC(),
		ID:        payload.ID,
	}, nil
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

// requireWriteAuth protects mutation endpoints with a Bearer token when an API
// key is configured. Read-only instances reject mutations even with a valid key.
func (s *Server) requireWriteAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkKey(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		if s.cfg.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			writeErr(w, http.StatusForbidden, errors.New("read-only mirror"))
			return
		}
		next(w, r)
	}
}

func (s *Server) checkKey(r *http.Request) bool {
	if s.cfg.APIKey == "" {
		return true
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	token = strings.TrimSpace(token)
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.APIKey)) == 1
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

func writeIntegrationUnavailable(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":      message,
		"error_code": "integration_unavailable",
	})
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
