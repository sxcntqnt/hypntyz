package server

import (
	"encoding/json"
	"net/http"
	"time"

	"hypnotz/internal/stream"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/stream", s.handleSSE)
	mux.HandleFunc("/subscribe", s.handleSubscribe)
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/stats", s.stats)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"clients":       s.streamHub.ClientCount(),
		"tick_rate_hz":  s.config.TickRateHz,
		"region":        s.config.RegionID,
		"backpressure":  s.config.EnableBackpressure,
		"max_vehicles":  s.config.MaxVehiclesPerClient,
		"max_clients":   s.config.MaxClientsPerNode,
		"timestamp":     time.Now().Unix(),
	})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		http.Error(w, "client_id required", http.StatusBadRequest)
		return
	}

	conn := s.streamHub.GetClient(clientID)
	if conn == nil {
		http.Error(w, "Client not found. Please subscribe first.", http.StatusNotFound)
		return
	}

	stream.ServeSSE(s.streamHub, clientID, w, r)
}
