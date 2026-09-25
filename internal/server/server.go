// Package server implements the HTTP API, WebSocket hub, and static
// frontend serving.
package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"github.com/jeffbstewart/unconf/internal/store"
)

// Config holds the server's dependencies.
type Config struct {
	Store         *store.Store
	EventID       string // the event this server instance serves
	AdminKey      string // UNCONF_ADMIN_KEY; must be non-empty
	SessionSecret []byte // HMAC key for session cookies
	Static        fs.FS  // compiled frontend (web/dist)
}

// Server is the root HTTP handler.
type Server struct {
	cfg Config
	mux *http.ServeMux
}

// New builds the server.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.Handle("POST /api/logout", s.requireUser(http.HandlerFunc(s.handleLogout)))
	s.mux.Handle("GET /api/me", s.requireUser(http.HandlerFunc(s.handleMe)))
	s.mux.Handle("/api/", http.NotFoundHandler())
	s.mux.Handle("/", newStaticHandler(cfg.Static))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
