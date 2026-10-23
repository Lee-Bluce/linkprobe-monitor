package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "lp_session"

type sessions struct {
	mu  sync.Mutex
	m   map[string]time.Time
	ttl time.Duration
}

func newSessions(ttl time.Duration) *sessions {
	if ttl <= 0 {
		ttl = 72 * time.Hour
	}
	return &sessions{m: make(map[string]time.Time), ttl: ttl}
}

func (s *sessions) Create() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	token := hex.EncodeToString(b[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.m {
		if now.After(exp) {
			delete(s.m, k)
		}
	}
	s.m[token] = now.Add(s.ttl)
	return token
}

func (s *sessions) Valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.m, token)
		return false
	}
	return true
}

func (s *sessions) Delete(token string) {
	s.mu.Lock()
	delete(s.m, token)
	s.mu.Unlock()
}

// rateLimiter blocks an address for a while after repeated login failures.
type rateLimiter struct {
	mu       sync.Mutex
	failures map[string]*failure
	max      int
	window   time.Duration
}

type failure struct {
	count int
	until time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{failures: make(map[string]*failure), max: max, window: window}
}

// Allowed reports whether the address may attempt a login now.
func (l *rateLimiter) Allowed(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.failures[ip]
	if !ok {
		return true
	}
	if time.Now().After(f.until) {
		delete(l.failures, ip)
		return true
	}
	return f.count < l.max
}

func (l *rateLimiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.failures[ip]
	if !ok || time.Now().After(f.until) {
		f = &failure{until: time.Now().Add(l.window)}
		l.failures[ip] = f
	}
	f.count++
}

func (l *rateLimiter) Clear(ip string) {
	l.mu.Lock()
	delete(l.failures, ip)
	l.mu.Unlock()
}

type loginRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if !s.limiter.Allowed(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "尝试次数过多，请稍后再试")
		return
	}
	var req loginRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "请输入密码")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(s.cfg.AdminPasswordHash), []byte(req.Password)) != nil {
		s.limiter.Fail(ip)
		// A small fixed delay makes guessing slower without a visible lockout.
		time.Sleep(300 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid_password", "密码不正确")
		return
	}
	s.limiter.Clear(ip)
	token := s.sess.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(time.Duration(s.cfg.SessionTTLHours) * time.Hour / time.Second),
		Secure:   r.TLS != nil,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sess.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
