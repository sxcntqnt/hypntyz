package server

import (
	"net/http"
	"hypnotz/internal/stream"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")

	stream.ServeSSE(s.streamHub, clientID, w, r)
}
