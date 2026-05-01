package stream

import (
	"sync"

	"hypnotz/internal/types"
)

type ClientConnection struct {
	ID       string
	Channel  chan []byte
	State    types.ClientState
	CloseCh  chan struct{}
}

type Hub struct {
	clients      map[string]*ClientConnection
	subscribers  map[string]chan types.ProjectionBatch
	mu           sync.RWMutex
	maxClients   int
	backpressure bool
}

func NewHub(maxClients int, enableBackpressure bool) *Hub {
	return &Hub{
		clients:      make(map[string]*ClientConnection),
		subscribers:  make(map[string]chan types.ProjectionBatch),
		maxClients:   maxClients,
		backpressure: enableBackpressure,
	}
}

func (h *Hub) AddClient(id string, state types.ClientState) *ClientConnection {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.clients) >= h.maxClients {
		if h.backpressure {
			return nil
		}
	}

	conn := &ClientConnection{
		ID:      id,
		Channel: make(chan []byte, 256),
		State:   state,
		CloseCh: make(chan struct{}),
	}

	h.clients[id] = conn
	return conn
}

func (h *Hub) RemoveClient(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conn, ok := h.clients[id]; ok {
		close(conn.CloseCh)
		close(conn.Channel)
		delete(h.clients, id)
	}
}

func (h *Hub) GetClient(id string) *ClientConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[id]
}

func (h *Hub) GetAllClients() []*ClientConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()

	clients := make([]*ClientConnection, 0, len(h.clients))
	for _, conn := range h.clients {
		clients = append(clients, conn)
	}
	return clients
}

func (h *Hub) Broadcast(batch types.ProjectionBatch) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, conn := range h.clients {
		select {
		case conn.Channel <- nil:
		default:
			if h.backpressure {
				close(conn.Channel)
				delete(h.clients, conn.ID)
			}
		}
	}
}

func (h *Hub) SendToClient(clientID string, data []byte) bool {
	h.mu.RLock()
	conn, ok := h.clients[clientID]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case conn.Channel <- data:
		return true
	default:
		return false
	}
}

func (h *Hub) UpdateClientState(id string, state types.ClientState) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conn, ok := h.clients[id]; ok {
		conn.State = state
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) IsUnderBackpressure() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.backpressure && len(h.clients) >= h.maxClients
}
