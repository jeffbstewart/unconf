// Package server implements the HTTP API, WebSocket hub, and static
// frontend serving.
package server

import (
	"context"
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
	RingSize      int    // events kept for reconnect replay; 0 = 10 000
}

// Server is the root HTTP handler. It owns the realtime hub, which runs
// until Close.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	hub    *Hub
	ctx    context.Context
	cancel context.CancelFunc
}

// New builds the server and starts its hub.
func New(cfg Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	hub, err := newHub(ctx, cfg.Store, cfg.EventID, cfg.RingSize)
	if err != nil {
		cancel()
		return nil, err
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux(), hub: hub, ctx: ctx, cancel: cancel}
	go hub.run()
	s.mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.Handle("POST /api/logout", s.requireUser(http.HandlerFunc(s.handleLogout)))
	s.mux.Handle("GET /api/me", s.requireUser(http.HandlerFunc(s.handleMe)))
	s.mux.Handle("GET /ws", s.requireUser(http.HandlerFunc(s.handleWS)))
	s.mux.Handle("/api/", http.NotFoundHandler())
	s.mux.Handle("/", newStaticHandler(cfg.Static))
	return s, nil
}

// Close disconnects all WebSocket clients and stops the hub.
func (s *Server) Close() {
	s.cancel()
	s.hub.wait()
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
