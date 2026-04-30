package stream

import (
	"fmt"
	"net/http"
)

func ServeSSE(h *Hub, clientID string, w http.ResponseWriter, r *http.Request) {

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", 500)
		return
	}

	ch := h.AddClient(clientID)
	defer h.RemoveClient(clientID)

	w.Header().Set("Content-Type", "text/event-stream")

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}
