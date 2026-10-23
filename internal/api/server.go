// Package api exposes the REST interface and serves the embedded front end.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"linkprobe/internal/config"
	"linkprobe/internal/pipeline"
	"linkprobe/internal/probe"
	"linkprobe/internal/store"
)

type Server struct {
	st      *store.Store
	cfg     config.Config
	sched   *probe.Scheduler
	writer  *pipeline.Writer
	agg     *pipeline.Aggregator
	log     *slog.Logger
	sess    *sessions
	limiter *rateLimiter
	mux     *http.ServeMux
	web     fs.FS
	version string
	build   string
	started time.Time
	clockOK func() bool
}

func New(st *store.Store, cfg config.Config, sched *probe.Scheduler, w *pipeline.Writer,
	agg *pipeline.Aggregator, webFS fs.FS, version, buildTime string, clockOK func() bool, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if clockOK == nil {
		clockOK = func() bool { return true }
	}
	s := &Server{
		st:      st,
		cfg:     cfg,
		sched:   sched,
		writer:  w,
		agg:     agg,
		log:     log,
		sess:    newSessions(time.Duration(cfg.SessionTTLHours) * time.Hour),
		limiter: newRateLimiter(5, 10*time.Minute),
		mux:     http.NewServeMux(),
		web:     webFS,
		version: version,
		build:   buildTime,
		started: time.Now(),
		clockOK: clockOK,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)

	s.mux.HandleFunc("GET /api/groups", s.auth(s.listGroups))
	s.mux.HandleFunc("POST /api/groups", s.auth(s.createGroup))
	s.mux.HandleFunc("PATCH /api/groups/{id}", s.auth(s.renameGroup))
	s.mux.HandleFunc("DELETE /api/groups/{id}", s.auth(s.deleteGroup))
	s.mux.HandleFunc("PUT /api/groups/order", s.auth(s.orderGroups))

	s.mux.HandleFunc("GET /api/nodes", s.auth(s.listNodes))
	s.mux.HandleFunc("POST /api/nodes", s.auth(s.createNode))
	s.mux.HandleFunc("GET /api/nodes/{id}", s.auth(s.getNode))
	s.mux.HandleFunc("PATCH /api/nodes/{id}", s.auth(s.patchNode))
	s.mux.HandleFunc("DELETE /api/nodes/{id}", s.auth(s.deleteNode))
	s.mux.HandleFunc("PUT /api/nodes/order", s.auth(s.orderNodes))
	s.mux.HandleFunc("POST /api/nodes/{id}/test", s.auth(s.testNode))
	s.mux.HandleFunc("POST /api/probe/test", s.auth(s.testDraft))

	s.mux.HandleFunc("GET /api/overview", s.auth(s.overview))
	s.mux.HandleFunc("GET /api/nodes/{id}/series", s.auth(s.series))
	s.mux.HandleFunc("GET /api/system", s.auth(s.system))

	// Unknown API paths must not fall through to the SPA shell.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	})

	// Registered without a method so it does not conflict with "/api/";
	// serveSPA still rejects anything but GET/HEAD.
	s.mux.HandleFunc("/", s.serveSPA)
}

func (s *Server) Handler() http.Handler {
	return s.recover(s.securityHeaders(s.logRequests(s.mux)))
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			lvl := slog.LevelDebug
			if rec.status >= 500 {
				lvl = slog.LevelWarn
			}
			s.log.Log(r.Context(), lvl, "api", "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "ms", elapsed.Milliseconds())
		}
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || !s.sess.Valid(c.Value) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		if !s.sameOrigin(r) {
			writeError(w, http.StatusForbidden, "origin", "请求来源不被允许")
			return
		}
		next(w, r)
	}
}

func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := parseOrigin(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u, r.Host)
}

func parseOrigin(origin string) (string, error) {
	switch {
	case strings.HasPrefix(origin, "http://"):
		return strings.TrimPrefix(origin, "http://"), nil
	case strings.HasPrefix(origin, "https://"):
		return strings.TrimPrefix(origin, "https://"), nil
	}
	return "", errors.New("bad origin")
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			if i := strings.IndexByte(v, ','); i > 0 {
				return strings.TrimSpace(v[:i])
			}
			return strings.TrimSpace(v)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// serveSPA serves the embedded build, falling back to index.html for client
// side routes. It never exposes directory listings.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method", "不支持的方法")
		return
	}
	if s.web == nil {
		http.Error(w, "frontend not built", http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	f, err := s.web.Open(name)
	if err == nil {
		defer f.Close()
		if st, err := f.Stat(); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeContent(w, r, st.Name(), st.ModTime(), f.(io.ReadSeeker))
			return
		}
	}
	if strings.HasPrefix(name, "assets/") {
		http.NotFound(w, r)
		return
	}
	index, err := s.web.Open("index.html")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusServiceUnavailable)
		return
	}
	defer index.Close()
	st, err := index.Stat()
	if err != nil {
		http.Error(w, "frontend not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", st.ModTime(), index.(io.ReadSeeker))
}

// --- small helpers -------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encoding error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf)
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var e apiError
	e.Error.Code = code
	e.Error.Message = msg
	writeJSON(w, status, e)
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("请求格式不正确")
	}
	return nil
}

func (s *Server) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	var id int64
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_id", "无效的编号")
		return 0, false
	}
	return id, true
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	authenticated := false
	if c, err := r.Cookie(sessionCookie); err == nil && s.sess.Valid(c.Value) {
		authenticated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": authenticated,
		"version":       s.version,
	})
}
