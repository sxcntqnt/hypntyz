package memory_test

import (
	"testing"
	"time"

	"hypnotz/internal/memory"
	"hypnotz/internal/types"
)

func TestMemoryEntityCreation(t *testing.T) {
	event := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
		Speed:     60.0,
		Heading:   1.5,
	}

	entity := memory.NewMemoryEntity("v1", event)

	if entity.ID != "v1" {
		t.Error("Entity ID should match")
	}

	if entity.SeenCount != 1 {
		t.Error("Initial seen count should be 1")
	}

	if entity.Salience < 0 || entity.Salience > 1 {
		t.Error("Salience should be bounded [0,1]")
	}
}

func TestMemoryEntityApply(t *testing.T) {
	event1 := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
		Speed:     60.0,
	}

	entity := memory.NewMemoryEntity("v1", event1)

	event2 := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7750,
		Lon:       -122.4195,
		Speed:     65.0,
	}

	entity.Apply(event2)

	if entity.SeenCount != 2 {
		t.Error("Seen count should increment")
	}

	if len(entity.Trajectory) != 2 {
		t.Error("Trajectory should have 2 points")
	}
}

func TestMemoryEntityDecay(t *testing.T) {
	event := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
	}

	entity := memory.NewMemoryEntity("v1", event)
	initialSalience := entity.Salience

	entity.Decay(10.0)

	if entity.Salience >= initialSalience {
		t.Error("Salience should decay over time")
	}
}

func TestMemoryStoreUpsert(t *testing.T) {
	config := memory.DefaultMemoryConfig()
	config.MaxEntities = 100
	store := memory.NewStore(config)
	defer store.Stop()

	event := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
	}

	store.Upsert(event)

	if store.Count() != 1 {
		t.Error("Store should have 1 entity")
	}

	entity, exists := store.Get("v1")
	if !exists {
		t.Error("Entity should exist")
	}

	if entity.SeenCount != 1 {
		t.Error("Seen count should be 1")
	}
}

func TestMemoryStoreUpsertBatch(t *testing.T) {
	config := memory.DefaultMemoryConfig()
	config.MaxEntities = 100
	store := memory.NewStore(config)
	defer store.Stop()

	events := []types.VehicleState{
		{VehicleID: "v1", Lat: 37.7749, Lon: -122.4194},
		{VehicleID: "v2", Lat: 37.7750, Lon: -122.4195},
		{VehicleID: "v3", Lat: 37.7751, Lon: -122.4196},
	}

	store.UpsertBatch(events)

	if store.Count() != 3 {
		t.Error("Store should have 3 entities")
	}
}

func TestMemoryStoreDecay(t *testing.T) {
	config := memory.DefaultMemoryConfig()
	config.MaxEntities = 100
	config.EnableDecay = false
	store := memory.NewStore(config)
	defer store.Stop()

	event := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
	}

	store.Upsert(event)
	store.DecayAll()

	entity, _ := store.Get("v1")
	if entity.Salience > 0.99 {
		t.Error("Salience should have decayed slightly")
	}
}

func TestMemoryStoreQuery(t *testing.T) {
	config := memory.DefaultMemoryConfig()
	config.MaxEntities = 100
	store := memory.NewStore(config)
	defer store.Stop()

	events := []types.VehicleState{
		{VehicleID: "v1", Lat: 37.7749, Lon: -122.4194},
		{VehicleID: "v2", Lat: 37.7750, Lon: -122.4195},
		{VehicleID: "v3", Lat: 40.7128, Lon: -74.0060},
	}

	store.UpsertBatch(events)

	clientState := types.ClientState{
		Viewport: types.Viewport{
			MinLat: 37.77,
			MinLon: -122.42,
			MaxLat: 37.78,
			MaxLon: -122.41,
		},
	}

	entities := store.Query(clientState)

	if len(entities) < 2 {
		t.Error("Should return entities in viewport")
	}
}

func TestMemoryEntityEmbedding(t *testing.T) {
	event := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
		Speed:     60.0,
		Heading:   1.5,
	}

	entity := memory.NewMemoryEntity("v1", event)
	embedding := entity.GetEmbedding()

	if len(embedding) == 0 {
		t.Error("Embedding should not be empty")
	}

	entity.UpdateEmbedding()
	embedding2 := entity.GetEmbedding()

	if len(embedding2) != len(embedding) {
		t.Error("Embedding dimension should be consistent")
	}
}

func TestMemoryEntityPrediction(t *testing.T) {
	event := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
		Speed:     10.0,
		Heading:   0,
	}

	entity := memory.NewMemoryEntity("v1", event)

	predicted := entity.Predict(1 * time.Second)

	if predicted.Lat != 37.7749 {
		t.Error("Prediction should update position")
	}
}

func TestMemoryEntityAnomaly(t *testing.T) {
	event := types.VehicleState{
		VehicleID:   "v1",
		Lat:         37.7749,
		Lon:         -122.4194,
		DataSource:  "merged",
	}

	entity := memory.NewMemoryEntity("v1", event)
	
	for i := 0; i < 3; i++ {
		entity.Apply(event)
	}

	if !entity.IsAnomalous() {
		t.Error("Entity should be anomalous after multiple events")
	}
}

func TestMemoryStoreGarbageCollect(t *testing.T) {
	config := memory.DefaultMemoryConfig()
	config.MaxEntities = 100
	config.EntityTTL = 1 * time.Millisecond
	config.EnableDecay = false
	store := memory.NewStore(config)
	defer store.Stop()

	event := types.VehicleState{
		VehicleID: "v1",
		Lat:       37.7749,
		Lon:       -122.4194,
	}

	store.Upsert(event)
	time.Sleep(10 * time.Millisecond)
	store.GarbageCollect()

	if store.Count() != 0 {
		t.Error("Stale entity should be collected")
	}
}
