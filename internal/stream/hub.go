package stream

import "sync"

type Hub struct {
	clients map[string]chan []byte
	mu      sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]chan []byte),
	}
}

func (h *Hub) AddClient(id string) chan []byte {
	ch := make(chan []byte, 64)

	h.mu.Lock()
	h.clients[id] = ch
	h.mu.Unlock()

	return ch
}

func (h *Hub) RemoveClient(id string) {
	h.mu.Lock()
	delete(h.clients, id)
	h.mu.Unlock()
}
