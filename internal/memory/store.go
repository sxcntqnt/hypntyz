package memory

import (
	"sync"
	"time"

	"hypnotz/internal/types"
)

type MemoryConfig struct {
	MaxEntities     int
	EntityTTL       time.Duration
	DecayInterval   time.Duration
	EnableDecay     bool
}

func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		MaxEntities:   100000,
		EntityTTL:     30 * time.Minute,
		DecayInterval: time.Second,
		EnableDecay:   true,
	}
}

type MemoryStore struct {
	entities map[string]*MemoryEntity
	mu       sync.RWMutex
	config   MemoryConfig
	stopCh   chan struct{}
}

func NewStore(config MemoryConfig) *MemoryStore {
	store := &MemoryStore{
		entities: make(map[string]*MemoryEntity),
		config:   config,
		stopCh:   make(chan struct{}),
	}

	if config.EnableDecay {
		go store.startDecayLoop()
	}

	return store
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

func (ms *MemoryStore) Upsert(event types.VehicleState) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if len(ms.entities) >= ms.config.MaxEntities {
		ms.GarbageCollect()
	}

	entity, exists := ms.entities[event.VehicleID]
	if !exists {
		entity = NewMemoryEntity(event.VehicleID, event)
		ms.entities[event.VehicleID] = entity
	} else {
		entity.Apply(event)
	}
}

func (ms *MemoryStore) UpsertBatch(events []types.VehicleState) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, event := range events {
		if len(ms.entities) >= ms.config.MaxEntities {
			ms.GarbageCollect()
		}

		entity, exists := ms.entities[event.VehicleID]
		if !exists {
			entity = NewMemoryEntity(event.VehicleID, event)
			ms.entities[event.VehicleID] = entity
		} else {
			entity.Apply(event)
		}
	}
}

func (ms *MemoryStore) Get(id string) (*MemoryEntity, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	entity, exists := ms.entities[id]
	return entity, exists
}

func (ms *MemoryStore) GetAll() []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	entities := make([]*MemoryEntity, 0, len(ms.entities))
	for _, entity := range ms.entities {
		entities = append(entities, entity)
	}

	return entities
}

func (ms *MemoryStore) Query(clientState types.ClientState) []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	entities := make([]*MemoryEntity, 0)

	for _, entity := range ms.entities {
		if entity.IsStale {
			continue
		}

		inViewport := entity.Position.Lat >= clientState.Viewport.MinLat &&
			entity.Position.Lat <= clientState.Viewport.MaxLat &&
			entity.Position.Lon >= clientState.Viewport.MinLon &&
			entity.Position.Lon <= clientState.Viewport.MaxLon

		if inViewport || entity.Salience > 0.7 || entity.IsAnomalous() {
			entities = append(entities, entity)
		}
	}

	return entities
}

func (ms *MemoryStore) DecayAll() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	now := time.Now()
	for _, entity := range ms.entities {
		staleness := now.Sub(entity.LastSeen)
		entity.Decay(staleness.Seconds())
	}
}

func (ms *MemoryStore) GarbageCollect() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for id, entity := range ms.entities {
		if entity.IsStale || time.Since(entity.LastSeen) > ms.config.EntityTTL {
			delete(ms.entities, id)
		}
	}
}

func (ms *MemoryStore) Count() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return len(ms.entities)
}

func (ms *MemoryStore) Stop() {
	close(ms.stopCh)
}

func (ms *MemoryStore) Reset() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.entities = make(map[string]*MemoryEntity)
}

func (ms *MemoryStore) GetStaleEntities() []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	stale := make([]*MemoryEntity, 0)
	for _, entity := range ms.entities {
		if entity.IsStale {
			stale = append(stale, entity)
		}
	}

	return stale
}

func (ms *MemoryStore) GetActiveEntities() []*MemoryEntity {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	active := make([]*MemoryEntity, 0)
	for _, entity := range ms.entities {
		if !entity.IsStale {
			active = append(active, entity)
		}
	}

	return active
}

func (ms *MemoryStore) ApplySequence(seq types.TensorSequence) (*MemoryEntity, error) {
	if len(seq.Tokens) == 0 {
		return nil, nil
	}

	state := types.VehicleState{
		VehicleID: seq.VehicleID,
	}

	if len(seq.Timestamps) > 0 {
		state.TimestampNS = seq.Timestamps[len(seq.Timestamps)-1]
		state.Timestamp = time.Unix(0, state.TimestampNS)
	}

	if len(seq.Tokens) > 0 {
		lastToken := seq.Tokens[len(seq.Tokens)-1]
		if len(lastToken) >= 2 {
			state.Lat = lastToken[0]
			state.Lon = lastToken[1]
		}
		if len(lastToken) >= 3 {
			state.Speed = lastToken[2]
		}
		if len(lastToken) >= 4 {
			state.Heading = lastToken[3]
		}
		if len(seq.Confidence) > 0 {
			state.Confidence = seq.Confidence[len(seq.Confidence)-1]
		}
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	entity, exists := ms.entities[seq.VehicleID]
	if !exists {
		entity = NewMemoryEntity(seq.VehicleID, state)
		ms.entities[seq.VehicleID] = entity
	} else {
		entity.Apply(state)
	}

	return entity, nil
}
