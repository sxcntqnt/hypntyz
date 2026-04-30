package server

import (
	"net/http"
	"hypnotz/internal/stream"
)

type Server struct {
	streamHub *stream.Hub
}

func NewServer() *Server {
	return &Server{
		streamHub: stream.NewHub(),
	}
}

func (s *Server) Run(addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/stream", s.handleSSE)
	mux.HandleFunc("/health", s.health)

	return http.ListenAndServe(addr, mux)
}
