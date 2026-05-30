package memory

import (
	"sync"
	"time"

	"hypnotz/internal/types"
)

// MemoryConfig holds tunables for the MemoryStore.
type MemoryConfig struct {
	MaxEntities   int
	EntityTTL     time.Duration
	DecayInterval time.Duration
	EnableDecay   bool
}

// DefaultMemoryConfig returns safe production defaults.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		MaxEntities:   100_000,
		EntityTTL:     30 * time.Minute,
		DecayInterval: time.Second,
		EnableDecay:   true,
	}
}

// MemoryStore is a concurrent map of MemoryEntities keyed by vehicle ID.
type MemoryStore struct {
	mu       sync.RWMutex
	entities map[string]*MemoryEntity
	config   MemoryConfig
	stopCh   chan struct{}
}

// NewStore creates a MemoryStore and starts the background decay loop when
// config.EnableDecay is true.
func NewStore(config MemoryConfig) *MemoryStore {
	ms := &MemoryStore{
		entities: make(map[string]*MemoryEntity),
		config:   config,
		stopCh:   make(chan struct{}),
	}
	if config.EnableDecay {
		go ms.startDecayLoop()
	}
	return ms
}

func (ms *MemoryStore) startDecayLoop() {
	ticker := time.NewTicker(ms.config.DecayInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ms.DecayAll()
		case <-ms.stopCh:
			return
		}
	}
}

// Upsert inserts or updates an entity for the given event. When the store is
// at capacity, stale entities are collected before inserting.
func (ms *MemoryStore) Upsert(event types.VehicleState) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if len(ms.entities) >= ms.config.MaxEntities {
		ms.gcLocked() // uses already-held lock
	}

	if entity, ok := ms.entities[event.VehicleID]; ok {
		entity.Apply(event)
	} else {
		ms.entities[event.VehicleID] = NewMemoryEntity(event.VehicleID, event)
	}
}

// UpsertBatch inserts or updates entities for a slice of events.
func (ms *MemoryStore) UpsertBatch(events []types.VehicleState) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, event := range events {
		if len(ms.entities) >= ms.config.MaxEntities {
			ms.gcLocked()
		}
		if entity, ok := ms.entities[event.VehicleID]; ok {
			entity.Apply(event)
		} else {
			ms.entities[event.VehicleID] = NewMemoryEntity(event.VehicleID, event)
		}
	}
}

// Get returns the entity for id, or (nil, false) if not present.
func (ms *MemoryStore) Get(id string) (*MemoryEntity, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	e, ok := ms.entities[id]
	return e, ok
}

// GetAll returns a snapshot slice of all entities.
func (ms *MemoryStore) GetAll() []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	out := make([]*MemoryEntity, 0, len(ms.entities))
	for _, e := range ms.entities {
		out = append(out, e)
	}
	return out
}

// Query returns entities that are either inside the client's viewport,
// highly salient, or anomalous.
func (ms *MemoryStore) Query(clientState types.ClientState) []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	vp := clientState.Viewport
	out := make([]*MemoryEntity, 0)
	for _, e := range ms.entities {
		if e.IsStale {
			continue
		}
		inViewport := e.Position.Lat >= vp.MinLat && e.Position.Lat <= vp.MaxLat &&
			e.Position.Lon >= vp.MinLon && e.Position.Lon <= vp.MaxLon
		if inViewport || e.Salience > 0.7 || e.IsAnomalous() {
			out = append(out, e)
		}
	}
	return out
}

// DecayAll applies time-based salience decay to every entity.
func (ms *MemoryStore) DecayAll() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	now := time.Now()
	for _, e := range ms.entities {
		e.Decay(now.Sub(e.LastSeen).Seconds())
	}
}

// GarbageCollect removes stale and TTL-expired entities. It is safe to call
// from outside the store (acquires its own lock). Internal callers that
// already hold the write lock should call gcLocked instead.
func (ms *MemoryStore) GarbageCollect() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.gcLocked()
}

// gcLocked is the lock-free inner GC pass. Caller must hold ms.mu.Lock().
func (ms *MemoryStore) gcLocked() {
	for id, e := range ms.entities {
		if e.IsStale || time.Since(e.LastSeen) > ms.config.EntityTTL {
			delete(ms.entities, id)
		}
	}
}

// Count returns the current number of tracked entities.
func (ms *MemoryStore) Count() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return len(ms.entities)
}

// Stop signals the background decay loop to exit.
func (ms *MemoryStore) Stop() { close(ms.stopCh) }

// Reset removes all entities.
func (ms *MemoryStore) Reset() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.entities = make(map[string]*MemoryEntity)
}

// GetStaleEntities returns entities marked as stale.
func (ms *MemoryStore) GetStaleEntities() []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	var out []*MemoryEntity
	for _, e := range ms.entities {
		if e.IsStale {
			out = append(out, e)
		}
	}
	return out
}

// GetActiveEntities returns entities not marked as stale.
func (ms *MemoryStore) GetActiveEntities() []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	var out []*MemoryEntity
	for _, e := range ms.entities {
		if !e.IsStale {
			out = append(out, e)
		}
	}
	return out
}

// ApplySequence applies the last token of a TensorSequence as a VehicleState
// update, used by the compiler stage.
func (ms *MemoryStore) ApplySequence(seq types.TensorSequence) (*MemoryEntity, error) {
	if len(seq.Tokens) == 0 {
		return nil, nil
	}

	state := types.VehicleState{VehicleID: seq.VehicleID}
	if len(seq.Timestamps) > 0 {
		state.TimestampNS = seq.Timestamps[len(seq.Timestamps)-1]
		state.Timestamp = time.Unix(0, state.TimestampNS)
	}
	if last := seq.Tokens[len(seq.Tokens)-1]; len(last) >= 2 {
		state.Lat = last[0]
		state.Lon = last[1]
		if len(last) >= 3 {
			state.Speed = last[2]
		}
		if len(last) >= 4 {
			state.Heading = last[3]
		}
		if len(seq.Confidence) > 0 {
			state.Confidence = seq.Confidence[len(seq.Confidence)-1]
		}
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if entity, ok := ms.entities[seq.VehicleID]; ok {
		entity.Apply(state)
		return entity, nil
	}
	entity := NewMemoryEntity(seq.VehicleID, state)
	ms.entities[seq.VehicleID] = entity
	return entity, nil
}
