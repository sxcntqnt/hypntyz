package engine

import (
	"context"
	"time"

	"hypnotz/internal/attention"
	"hypnotz/internal/compiler"
	"hypnotz/internal/features"
	"hypnotz/internal/memory"
	"hypnotz/internal/ranker"
	"hypnotz/internal/sirtebasin"
	"hypnotz/internal/types"
	"hypnotz/internal/window"
)

type ProjectionEngine struct {
	adapter      *sirtebasin.SirtebasinAdapter
	windowing    *window.WindowingEngine
	tsc          *compiler.TemporalSequenceCompiler
	attentionEng *attention.Engine
	rank         *ranker.Ranker
	vectorPool   *features.VectorPool
	memoryStore  *memory.MemoryStore
	config       EngineConfig
}

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

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		TickRateHz:           20,
		MaxVehiclesPerClient: 500,
		MaxClientsPerNode:    10000,
		EnableBackpressure:   true,
		RegionID:             "default",
		WindowSizeNS:         60 * int64(time.Second),
		SlideNS:              30 * int64(time.Second),
		AllowedLatenessNS:    5 * int64(time.Second),
		MaxMemoryEntities:    100000,
		MemoryEntityTTL:      30 * time.Minute,
	}
}

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
	attentionCfg := attention.DefaultAttentionConfig()
	attentionEng := attention.NewEngine(attentionCfg, vectorPool)
	tsc := compiler.NewTemporalSequenceCompiler()
	rank := ranker.NewRanker(cfg.MaxVehiclesPerClient)

	memCfg := memory.DefaultMemoryConfig()
	memCfg.MaxEntities = cfg.MaxMemoryEntities
	memCfg.EntityTTL = cfg.MemoryEntityTTL
	memStore := memory.NewStore(memCfg)

	return &ProjectionEngine{
		adapter:      adapter,
		windowing:    window.NewWindowingEngine(windowPolicy),
		tsc:          tsc,
		attentionEng: attentionEng,
		rank:         rank,
		vectorPool:   vectorPool,
		memoryStore:  memStore,
		config:       cfg,
	}, nil
}

func (pe *ProjectionEngine) ProcessTick(ctx context.Context, clientState types.ClientState) ([]types.Projection, error) {
	queryReq := types.QueryRequest{
		QueryID:   clientState.ID,
		VehicleID: "",
		TStart:    time.Now().Add(-5 * time.Minute).UnixNano(),
		TEnd:      time.Now().UnixNano(),
		Mode:      types.HybridReconcile,
		FocusLat:  clientState.FocusLat,
		FocusLon:  clientState.FocusLon,
		TimeRange: types.TimeRange{
			Start: time.Now().Add(-5 * time.Minute),
			End:   time.Now(),
		},
	}

	stateSet, err := pe.adapter.Query(ctx, queryReq)
	if err != nil {
		return nil, err
	}

	for _, vehicle := range stateSet.Vehicles {
		event := window.StreamEvent{
			State:         vehicle,
			ArrivalTimeNS: time.Now().UnixNano(),
		}
		pe.windowing.Ingest(event)
	}

	pe.windowing.AdvanceWatermark(time.Now().UnixNano())

	windows, err := pe.windowing.Emit()
	if err != nil {
		return nil, err
	}

	allProjections := make([]types.Projection, 0)
	for _, w := range windows {
		if len(w.States) == 0 {
			continue
		}

		seq, err := pe.tsc.Compile(w.VehicleID, w.States)
		if err != nil {
			continue
		}

		entity, err := pe.memoryStore.ApplySequence(seq)
		if err != nil {
			continue
		}

		entities := []*memory.MemoryEntity{entity}
		scores := pe.attentionEng.QueryMemory(clientState, entities)

		for i, e := range entities {
			score := 0.0
			if i < len(scores) {
				score = scores[i]
			}

			proj := types.Projection{
				ID:        e.ID,
				Lat:       e.Position.Lat,
				Lon:       e.Position.Lon,
				Score:     score,
				Speed:     e.Velocity.Speed,
				Timestamp: e.LastSeen.UnixNano(),
			}
			allProjections = append(allProjections, proj)
		}
	}

	_ = pe.rank

	return allProjections, nil
}

func (pe *ProjectionEngine) scoreFromSequence(seq types.TensorSequence, clientState types.ClientState) []float64 {
	scores := make([]float64, len(seq.Tokens))

	for i := range seq.Tokens {
		lat := seq.Tokens[i][0]
		lon := seq.Tokens[i][1]

		dx := lat - clientState.FocusLat
		dy := lon - clientState.FocusLon
		distance := (dx*dx + dy*dy)

		inViewport := lat >= clientState.Viewport.MinLat &&
			lat <= clientState.Viewport.MaxLat &&
			lon >= clientState.Viewport.MinLon &&
			lon <= clientState.Viewport.MaxLon

		score := 0.5
		if inViewport {
			score += 0.3
		}

		score -= distance * 0.1

		if score > 1.0 {
			score = 1.0
		}
		if score < 0.0 {
			score = 0.0
		}

		scores[i] = score
	}

	return scores
}

func (pe *ProjectionEngine) GetWindowStats() (active, finalized int) {
	return 0, 0
}

func (pe *ProjectionEngine) Reset() {
	if pe.windowing != nil {
		pe.windowing.Reset()
	}
	if pe.memoryStore != nil {
		pe.memoryStore.Reset()
	}
}

func (pe *ProjectionEngine) GetMemoryStore() *memory.MemoryStore {
	return pe.memoryStore
}

func (pe *ProjectionEngine) Stop() {
	if pe.memoryStore != nil {
		pe.memoryStore.Stop()
	}
}
