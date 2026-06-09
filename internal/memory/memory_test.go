package memory_test

import (
	"testing"
	"time"

	"hypnotz/internal/memory"
	"hypnotz/internal/spectral"
	"hypnotz/internal/types"
)

// ─── MemoryEntity creation ─────────────────────────────────────────────────────

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
		t.Error("entity ID should match")
	}
	if entity.SeenCount != 1 {
		t.Error("initial seen count should be 1")
	}
	if entity.Salience < 0 || entity.Salience > 1 {
		t.Errorf("salience out of [0,1]: %f", entity.Salience)
	}
}

func TestMemoryEntitySpectralFieldsInitialised(t *testing.T) {
	event := types.VehicleState{VehicleID: "v1", Speed: 15.0}
	entity := memory.NewMemoryEntity("v1", event)

	if entity.SpeedHistory == nil {
		t.Fatal("SpeedHistory must be initialised on NewMemoryEntity")
	}
	if entity.SpectralProfile == nil {
		t.Fatal("SpectralProfile must be initialised on NewMemoryEntity")
	}
	// Fresh entity: ring buffer has one sample from construction position
	// but spectral profile should not yet be computed (below MinSamples).
	if entity.SpectralProfile.LastUpdatedNS != 0 {
		t.Error("SpectralProfile should not be computed on a brand-new entity")
	}
}

// ─── Apply ─────────────────────────────────────────────────────────────────────

func TestMemoryEntityApply(t *testing.T) {
	entity := memory.NewMemoryEntity("v1", types.VehicleState{
		VehicleID: "v1", Lat: 37.7749, Lon: -122.4194, Speed: 60.0,
	})
	entity.Apply(types.VehicleState{
		VehicleID: "v1", Lat: 37.7750, Lon: -122.4195, Speed: 65.0,
	})

	if entity.SeenCount != 2 {
		t.Errorf("seen count should be 2, got %d", entity.SeenCount)
	}
	if len(entity.Trajectory) != 2 {
		t.Errorf("trajectory should have 2 points, got %d", len(entity.Trajectory))
	}
}

func TestMemoryEntityApplyPopulatesRingBuffer(t *testing.T) {
	entity := memory.NewMemoryEntity("v1", types.VehicleState{VehicleID: "v1", Speed: 10.0})

	for i := 0; i < 5; i++ {
		entity.Apply(types.VehicleState{VehicleID: "v1", Speed: float64(10 + i)})
	}

	if entity.SpeedHistory.Len() < 5 {
		t.Errorf("ring buffer should have at least 5 samples, got %d", entity.SpeedHistory.Len())
	}
}

func TestMemoryEntitySpectralProfileUpdatedAfterMinSamples(t *testing.T) {
	entity := memory.NewMemoryEntity("v1", types.VehicleState{VehicleID: "v1", Speed: 15.0})

	// Push enough samples to exceed DefaultMinSamples (16).
	for i := 0; i < spectral.DefaultMinSamples+2; i++ {
		entity.Apply(types.VehicleState{
			VehicleID: "v1",
			Speed:     15.0 + float64(i%3)*0.5,
		})
	}

	if entity.SpectralProfile.LastUpdatedNS == 0 {
		t.Error("SpectralProfile should be updated once ring buffer has enough samples")
	}
}

func TestMemoryEntitySpectralProfileNotUpdatedBeforeMinSamples(t *testing.T) {
	entity := memory.NewMemoryEntity("v1", types.VehicleState{VehicleID: "v1", Speed: 15.0})

	// Push fewer than DefaultMinSamples.
	for i := 0; i < spectral.DefaultMinSamples-2; i++ {
		entity.Apply(types.VehicleState{VehicleID: "v1", Speed: 15.0})
	}

	if entity.SpectralProfile.LastUpdatedNS != 0 {
		t.Error("SpectralProfile should not be updated below DefaultMinSamples")
	}
}

// ─── Decay ─────────────────────────────────────────────────────────────────────

func TestMemoryEntityDecay(t *testing.T) {
	entity := memory.NewMemoryEntity("v1", types.VehicleState{
		VehicleID: "v1", Lat: 37.7749, Lon: -122.4194,
	})
	initial := entity.Salience
	entity.Decay(10.0)
	if entity.Salience >= initial {
		t.Error("salience should decrease after Decay")
	}
}

// ─── Anomaly ───────────────────────────────────────────────────────────────────

func TestMemoryEntityAnomalyUsesTypedConstant(t *testing.T) {
	// DataSource must use types.SourceMerged (typed constant), not a raw
	// string literal — this test will fail to compile if the constant is wrong.
	event := types.VehicleState{
		VehicleID:  "v1",
		Lat:        37.7749,
		Lon:        -122.4194,
		DataSource: types.SourceMerged, // ← was "merged" (raw string) — fixed
	}
	entity := memory.NewMemoryEntity("v1", event)
	for i := 0; i < 3; i++ {
		entity.Apply(event)
	}
	if !entity.IsAnomalous() {
		t.Error("entity should be anomalous after multiple merged-source events")
	}
}

// ─── Embedding ─────────────────────────────────────────────────────────────────

func TestMemoryEntityEmbedding(t *testing.T) {
	entity := memory.NewMemoryEntity("v1", types.VehicleState{
		VehicleID: "v1", Lat: 37.7749, Lon: -122.4194, Speed: 60.0, Heading: 1.5,
	})
	emb := entity.GetEmbedding()
	if len(emb) == 0 {
		t.Error("embedding should not be empty")
	}
	entity.UpdateEmbedding()
	emb2 := entity.GetEmbedding()
	if len(emb2) != len(emb) {
		t.Error("embedding dimension should be stable across updates")
	}
}

// ─── Prediction ────────────────────────────────────────────────────────────────

func TestMemoryEntityPrediction(t *testing.T) {
	entity := memory.NewMemoryEntity("v1", types.VehicleState{
		VehicleID: "v1", Lat: 37.7749, Lon: -122.4194, Speed: 10.0,
	})
	// A freshly created entity has Dx=Dy=0 so prediction stays at origin.
	predicted := entity.Predict(1 * time.Second)
	if predicted.Lat != 37.7749 {
		t.Errorf("static entity prediction: expected Lat=37.7749, got %f", predicted.Lat)
	}
}

// ─── MemoryStore ───────────────────────────────────────────────────────────────

func TestMemoryStoreUpsert(t *testing.T) {
	store := memory.NewStore(memory.DefaultMemoryConfig())
	defer store.Stop()

	store.Upsert(types.VehicleState{VehicleID: "v1", Lat: 37.7749, Lon: -122.4194})

	if store.Count() != 1 {
		t.Errorf("store should have 1 entity, got %d", store.Count())
	}
	entity, exists := store.Get("v1")
	if !exists {
		t.Fatal("entity should exist after Upsert")
	}
	if entity.SeenCount != 1 {
		t.Errorf("seen count should be 1, got %d", entity.SeenCount)
	}
}

func TestMemoryStoreUpsertBatch(t *testing.T) {
	store := memory.NewStore(memory.DefaultMemoryConfig())
	defer store.Stop()

	store.UpsertBatch([]types.VehicleState{
		{VehicleID: "v1", Lat: 37.7749, Lon: -122.4194},
		{VehicleID: "v2", Lat: 37.7750, Lon: -122.4195},
		{VehicleID: "v3", Lat: 37.7751, Lon: -122.4196},
	})

	if store.Count() != 3 {
		t.Errorf("store should have 3 entities, got %d", store.Count())
	}
}

func TestMemoryStoreDecay(t *testing.T) {
	cfg := memory.DefaultMemoryConfig()
	cfg.EnableDecay = false
	store := memory.NewStore(cfg)
	defer store.Stop()

	store.Upsert(types.VehicleState{VehicleID: "v1", Lat: 37.7749, Lon: -122.4194})
	store.DecayAll()

	entity, _ := store.Get("v1")
	if entity.Salience > 0.99 {
		t.Errorf("salience should have decayed, got %f", entity.Salience)
	}
}

func TestMemoryStoreQuery(t *testing.T) {
	store := memory.NewStore(memory.DefaultMemoryConfig())
	defer store.Stop()

	store.UpsertBatch([]types.VehicleState{
		{VehicleID: "v1", Lat: 37.7749, Lon: -122.4194},
		{VehicleID: "v2", Lat: 37.7750, Lon: -122.4195},
		{VehicleID: "v3", Lat: 40.7128, Lon: -74.0060}, // New York — outside viewport
	})

	entities := store.Query(types.ClientState{
		Viewport: types.Viewport{
			MinLat: 37.77, MinLon: -122.42,
			MaxLat: 37.78, MaxLon: -122.41,
		},
	})
	if len(entities) < 2 {
		t.Errorf("expected ≥ 2 entities in viewport, got %d", len(entities))
	}
}

func TestMemoryStoreGarbageCollect(t *testing.T) {
	cfg := memory.DefaultMemoryConfig()
	cfg.EntityTTL = 1 * time.Millisecond
	cfg.EnableDecay = false
	store := memory.NewStore(cfg)
	defer store.Stop()

	store.Upsert(types.VehicleState{VehicleID: "v1", Lat: 37.7749, Lon: -122.4194})
	time.Sleep(10 * time.Millisecond)
	store.GarbageCollect()

	if store.Count() != 0 {
		t.Errorf("stale entity should be collected, store has %d", store.Count())
	}
}

func TestMemoryStoreGetActiveVsStale(t *testing.T) {
	cfg := memory.DefaultMemoryConfig()
	cfg.EnableDecay = false
	store := memory.NewStore(cfg)
	defer store.Stop()

	store.UpsertBatch([]types.VehicleState{
		{VehicleID: "v1"},
		{VehicleID: "v2"},
	})

	active := store.GetActiveEntities()
	stale := store.GetStaleEntities()
	if len(active) != 2 {
		t.Errorf("expected 2 active, got %d", len(active))
	}
	if len(stale) != 0 {
		t.Errorf("expected 0 stale, got %d", len(stale))
	}
}
