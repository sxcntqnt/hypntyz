package engine

import (
	"context"
	"log/slog"
	"time"

	"hypnotz/internal/attention"
	"hypnotz/internal/compiler"
	"hypnotz/internal/features"
	"hypnotz/internal/memory"
	"hypnotz/internal/ranker"
	"hypnotz/internal/sirtebasin"
	"hypnotz/internal/spectral"
	"hypnotz/internal/types"
	"hypnotz/internal/window"
)

// ProjectionEngine is the main orchestrator. Each call to ProcessTick drives
// the full cognitive pipeline for one client at the current moment in time.
type ProjectionEngine struct {
	adapter          *sirtebasin.SirtebasinAdapter
	windowing        *window.WindowingEngine
	tsc              *compiler.TemporalSequenceCompiler
	spectralCompiler *spectral.SpectralFeatureCompiler // frequency-domain enrichment stage
	attentionEng     *attention.Engine
	rank             *ranker.Ranker
	vectorPool       *features.VectorPool
	memoryStore      *memory.MemoryStore
	config           EngineConfig
}

// EngineConfig holds all tunables for a ProjectionEngine node.
type EngineConfig struct {
	TickRateHz           int
	MaxVehiclesPerClient int
	MaxClientsPerNode    int
	EnableBackpressure   bool
	RegionID             string
	SirtebasinURL        string
	RedisURL             string
	ClickHouseHost       string
	WindowSizeNS         int64
	SlideNS              int64
	AllowedLatenessNS    int64
	MaxMemoryEntities    int
	MemoryEntityTTL      time.Duration
}

// DefaultEngineConfig returns safe production defaults.
// SirtebasinURL, RedisURL, and ClickHouseHost must be set before use.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		TickRateHz:           20,
		MaxVehiclesPerClient: 500,
		MaxClientsPerNode:    10_000,
		EnableBackpressure:   true,
		RegionID:             "default",
		WindowSizeNS:         60 * int64(time.Second),
		SlideNS:              30 * int64(time.Second),
		AllowedLatenessNS:    5 * int64(time.Second),
		MaxMemoryEntities:    100_000,
		MemoryEntityTTL:      30 * time.Minute,
	}
}

// NewProjectionEngine constructs and wires the full pipeline.
func NewProjectionEngine(cfg EngineConfig) (*ProjectionEngine, error) {
	adapterCfg := sirtebasin.AdapterConfig{
		ClickHouseTimeout:  200 * time.Millisecond,
		StabilityThreshold: 5 * time.Minute,
		RedisURL:           cfg.RedisURL,
		ClickHouseHost:     cfg.ClickHouseHost,
	}
	adapter, err := sirtebasin.NewAdapter(adapterCfg)
	if err != nil {
		return nil, err
	}

	windowPolicy := window.WindowPolicy{
		WindowSizeNS:      cfg.WindowSizeNS,
		SlideNS:           cfg.SlideNS,
		AllowedLatenessNS: cfg.AllowedLatenessNS,
	}

	vectorPool := features.NewVectorPool(features.FeatureSize)

	memCfg := memory.DefaultMemoryConfig()
	memCfg.MaxEntities = cfg.MaxMemoryEntities
	memCfg.EntityTTL = cfg.MemoryEntityTTL
	memStore := memory.NewStore(memCfg)

	return &ProjectionEngine{
		adapter:          adapter,
		windowing:        window.NewWindowingEngine(windowPolicy),
		tsc:              compiler.NewTemporalSequenceCompiler(),
		spectralCompiler: spectral.NewDefaultCompiler(),
		attentionEng:     attention.NewEngine(attention.DefaultAttentionConfig(), vectorPool),
		rank:             ranker.NewRanker(cfg.MaxVehiclesPerClient),
		vectorPool:       vectorPool,
		memoryStore:      memStore,
		config:           cfg,
	}, nil
}

// ProcessTick runs the full cognitive pipeline for clientState and returns
// the top-K ranked projections for that client.
//
// Pipeline stages:
//
//	Sirtebasin query
//	  → Windowing (temporal segmentation)
//	    → Temporal Sequence Compiler  (VehicleState[] → TensorSequence)
//	      → Spectral Feature Compiler (TensorSequence → EnrichedSequence)
//	        → Memory Engine           (entity upsert + spectral profile update)
//	          → Attention Engine      (geo + salience + spectral scoring)
//	            → Ranker              (top-K thinning)
func (pe *ProjectionEngine) ProcessTick(ctx context.Context, clientState types.ClientState) ([]types.Projection, error) {
	queryReq := types.QueryRequest{
		QueryID:  clientState.ID,
		Mode:     types.HybridReconcile,
		FocusLat: clientState.FocusLat,
		FocusLon: clientState.FocusLon,
		TStart:   time.Now().Add(-5 * time.Minute).UnixNano(),
		TEnd:     time.Now().UnixNano(),
		TimeRange: types.TimeRange{
			Start: time.Now().Add(-5 * time.Minute),
			End:   time.Now(),
		},
	}

	stateSet, err := pe.adapter.Query(ctx, queryReq)
	if err != nil {
		return nil, err
	}

	// ── Stage 1: Windowing ───────────────────────────────────────────────────
	now := time.Now().UnixNano()
	for _, vehicle := range stateSet.Vehicles {
		pe.windowing.Ingest(window.StreamEvent{
			State:         vehicle,
			ArrivalTimeNS: now,
		})
	}
	pe.windowing.AdvanceWatermark(now)

	windows, err := pe.windowing.Emit()
	if err != nil {
		return nil, err
	}

	// ── Stages 2–5: Compile → Spectral → Memory → Score ─────────────────────
	projections := make([]types.Projection, 0, len(windows))

	for _, w := range windows {
		if len(w.States) == 0 {
			continue
		}

		// Stage 2: Temporal Sequence Compiler
		seq, err := pe.tsc.Compile(w.VehicleID, w.States)
		if err != nil {
			slog.Warn("temporal compile failed",
				"vehicle", w.VehicleID, "err", err)
			continue
		}

		// Stage 3: Memory Engine upsert.
		// Apply first so the entity's ring buffer and SpectralProfile are
		// updated with the latest speed sample before the spectral compiler
		// reads the full history.
		entity, err := pe.memoryStore.ApplySequence(seq)
		if err != nil || entity == nil {
			slog.Warn("memory apply failed",
				"vehicle", w.VehicleID, "err", err)
			continue
		}

		// Stage 4: Spectral Feature Compiler.
		// Merges entity speed history (ring buffer) with the current
		// sequence's speed tokens for a cross-tick FFT view.
		var speedHistory []float64
		if entity.SpeedHistory != nil && entity.SpeedHistory.Len() > 0 {
			speedHistory = entity.SpeedHistory.Slice()
		}
		enriched := pe.spectralCompiler.Compile(seq, speedHistory)
		if !enriched.Valid {
			// Not enough data for a full spectral frame yet.
			// The entity still has its ring-buffer SpectralProfile which
			// the attention engine will use.
			slog.Debug("spectral frame not yet valid",
				"vehicle", w.VehicleID,
				"history_samples", len(speedHistory))
		}

		// Stage 5: Attention scoring.
		// ScoreEntity reads entity.SpectralProfile.AnomalyScore (updated in
		// Stage 3) and blends it with geometric relevance and salience.
		score := pe.attentionEng.ScoreEntity(entity, clientState)

		projections = append(projections, types.Projection{
			ID:        entity.ID,
			Lat:       entity.Position.Lat,
			Lon:       entity.Position.Lon,
			Score:     score,
			Speed:     entity.Velocity.Speed,
			Heading:   entity.Velocity.Heading,
			Timestamp: entity.LastSeen.UnixNano(),
		})
	}

	// ── Stage 6: Ranker — top-K thinning ─────────────────────────────────────
	return pe.rank.Rank(projections), nil
}

// GetMemoryStore returns the engine's MemoryStore, used by the traffic API
// layer and diagnostics.
func (pe *ProjectionEngine) GetMemoryStore() *memory.MemoryStore {
	return pe.memoryStore
}

// GetWindowStats returns active and finalized window counts.
func (pe *ProjectionEngine) GetWindowStats() (active, finalized int) {
	return pe.windowing.Stats()
}

// Reset clears transient engine state (windowing buffers, memory store).
// The spectral compiler is stateless and does not need resetting.
func (pe *ProjectionEngine) Reset() {
	if pe.windowing != nil {
		pe.windowing.Reset()
	}
	if pe.memoryStore != nil {
		pe.memoryStore.Reset()
	}
}

// Stop shuts down background goroutines (memory decay loop, etc.).
func (pe *ProjectionEngine) Stop() {
	if pe.memoryStore != nil {
		pe.memoryStore.Stop()
	}
}
