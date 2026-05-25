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
	mux.HandleFunc("/diagnostics", s.handleDiagnostics)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	stats := map[string]interface{}{
		"clients":       s.streamHub.ClientCount(),
		"tick_rate_hz":  s.config.TickRateHz,
		"region":        s.config.RegionID,
		"backpressure":  s.config.EnableBackpressure,
		"max_vehicles":  s.config.MaxVehiclesPerClient,
		"max_clients":   s.config.MaxClientsPerNode,
		"timestamp":     time.Now().Unix(),
	}

	if s.projEngine != nil {
		memStore := s.projEngine.GetMemoryStore()
		if memStore != nil {
			stats["memory_entities"] = memStore.Count()
			stats["memory_active"] = len(memStore.GetActiveEntities())
			stats["memory_stale"] = len(memStore.GetStaleEntities())
		}
	}

	json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if s.projEngine == nil {
		http.Error(w, "Projection engine not initialized", http.StatusInternalServerError)
		return
	}

	memStore := s.projEngine.GetMemoryStore()
	if memStore == nil {
		http.Error(w, "Memory store not initialized", http.StatusInternalServerError)
		return
	}

	// Run diagnostics
	// Note: In real implementation, this would call the actual analyzer
	// For now, return placeholder metrics
	metrics := map[string]interface{}{
		"status": "diagnostics_available",
		"message": "Full diagnostics require live data stream",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
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
