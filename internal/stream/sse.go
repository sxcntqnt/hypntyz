package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"hypnotz/internal/types"
)

func ServeSSE(h *Hub, clientID string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	conn := h.GetClient(clientID)
	if conn == nil {
		http.Error(w, "Client not found", http.StatusNotFound)
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case data, ok := <-conn.Channel:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			return

		case <-conn.CloseCh:
			return
		}
	}
}

func BroadcastProjection(h *Hub, batch types.ProjectionBatch) {
	_, err := json.Marshal(batch)
	if err != nil {
		return
	}

	h.Broadcast(batch)
}

func SendToClient(h *Hub, clientID string, batch types.ProjectionBatch) bool {
	data, err := json.Marshal(batch)
	if err != nil {
		return false
	}

	return h.SendToClient(clientID, data)
}
