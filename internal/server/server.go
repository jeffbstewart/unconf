// Package server implements the HTTP API, WebSocket hub, and static
// frontend serving.
package server

import (
	"io/fs"
	"net/http"
)

// Server is the root HTTP handler.
type Server struct {
	mux *http.ServeMux
}

// New builds the server. static is the compiled frontend (web/dist).
func New(static fs.FS) *Server {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	s.mux.Handle("/api/", http.NotFoundHandler())
	s.mux.Handle("/", newStaticHandler(static))
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
