# hypntyz

A real-time vehicle projection engine with persistent cognitive memory and traffic modelling. hypntyz ingests high-frequency GPS telemetry, runs it through a stateful cognitive pipeline, and streams scored, ranked vehicle projections to connected clients over Server-Sent Events. It is designed to handle tens of thousands of concurrent clients and hundreds of thousands of tracked vehicles on a single node.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    CLIENT LAYER                             │
│  POST /subscribe  →  Client viewport + preferences          │
│  GET  /stream     →  SSE real-time updates                  │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                  TICK LOOP (20 Hz)                          │
│  - Runs every 50 ms                                         │
│  - Fetches active clients                                   │
│  - Triggers cognitive pipeline                              │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              SIRTEBASIN QUERY ADAPTER                       │
│  Modes: Realtime | Historical | Hybrid                      │
│  - Merges Redis (speed) + ClickHouse (batch)                │
│  - Timestamp dominance + confidence scoring                 │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              WINDOWING POLICY ENGINE                        │
│  - Temporal segmentation                                    │
│  - Watermarking for late events                             │
│  - Deterministic replay                                     │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              TEMPORAL SEQUENCE COMPILER                     │
│  - VehicleState[] → TensorSequence                          │
│  - Feature vector: [lat, lon, speed, heading,               │
│    confidence, time_delta, source]                          │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│           MEMORY ENGINE + TRAFFIC MODELLING                 │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  MemoryEntity with persistent state:                 │  │
│  │  • Position, velocity, trajectory                    │  │
│  │  • Salience, risk score, anomaly count               │  │
│  │  • Embedding (latent representation)                 │  │
│  │  • Attention history                                 │  │
│  │  • Predictive state                                  │  │
│  │                                                      │  │
│  │  TRAFFIC MODELLING                                   │  │
│  │  • TripLine crossing detection                       │  │
│  │  • Speed sample generation                           │  │
│  │  • Per-segment speed histograms                      │  │
│  │  • Anomaly detection via speed deviation             │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  Operations:                                                │
│  • Upsert(event)              → evolve entity state        │
│  • ProcessTrafficCrossing()   → detect speed anomalies     │
│  • Decay()                    → gradual salience fade      │
│  • Query(client)              → retrieve relevant entities │
│  • GarbageCollect()           → remove stale entities      │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              ATTENTION ENGINE                               │
│  - Scores MemoryEntities (not raw vehicles)                 │
│  - Combines:                                                │
│    • Geometric relevance (60%)                              │
│    • Entity salience (40%)                                  │
│    • Traffic anomaly boost                                  │
│  - Records attention history per entity                     │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              RANKER + THINNING                              │
│  - Sort by combined score                                   │
│  - Prioritize anomalies                                     │
│  - Top-K selection                                          │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              SSE BROADCAST                                  │
│  { "client_id": "abc", "vehicles": [...] }                  │
└─────────────────────────────────────────────────────────────┘
```

---

## Key innovation: memory + traffic cognition

### Before (stateless)

```
event → score → emit → discard
```

Every tick recomputes from scratch. No continuity between observations.

### After (cognitive + traffic modelling)

```
event → memory.Upsert() → traffic.ProcessCrossing() → attention.Score() → emit
              ↓                      ↓
   Persistent entity          Speed samples
   Trajectory tracking        Histogram aggregation
   Salience accumulation      Anomaly detection
   Embedding evolution        Risk score propagation
```

The engine now has **memory**, **attention**, and **traffic-aware cognition**.

---

## Package layout

```
internal/
├── types/              Shared wire types and constants
│   ├── types.go        Viewport, ClientState, Vehicle, Projection, FeatureVector
│   └── vehicle_state.go  VehicleState, TensorSequence, QueryRequest, SourceType
│
├── trafficmodel/       Pure traffic data types — no internal imports
│   ├── tripline.go     TripLine, Crossing, SpeedSample, geo helpers
│   └── histogram.go    SpeedHistogram (168 h × 120 speed bins)
│
├── memory/             In-process entity store
│   ├── entity.go       MemoryEntity: kinematics, salience, traffic model
│   ├── store.go        MemoryStore: concurrent map, decay loop, GC
│   └── embedding.go    EmbeddingEngine: cosine similarity, clustering
│
├── compiler/           VehicleState[] → TensorSequence
│   └── temporal_compiler.go
│
├── features/           Feature vector construction
│   ├── builder.go      FeatureVector builder, Haversine distance
│   └── vector.go       VectorPool / MatrixPool (sync.Pool wrappers)
│
├── sirtebasin/         Data layer adapter (Redis + ClickHouse)
│   ├── merge_engine.go Merge, Resolve, Deduplicate, confidence scoring
│   └── redis_client.go Redis client (QueryRedis, GetLatestState)
│
├── traffic/            Traffic API and persistence
│   ├── api.go          8 HTTP endpoints + SSE anomaly stream
│   ├── persistence.go  SQLite: speed_samples + anomalies tables
│   └── spatial_index.go  H3 spatial index (resolution 9, ~0.1 km²)
│
├── attention/          Attention scoring engine
├── window/             Windowing policy engine
├── ranker/             Ranking and top-K thinning
├── engine/             Tick loop orchestration
├── stream/             SSE connection management
├── server/             HTTP server wiring
└── diagnostics/        Observability and segment coverage analysis
```

### Import graph (no cycles)

```
stdlib
  └── types
        └── trafficmodel          ← zero internal imports
              └── memory
                    └── traffic   ← attention, ranker, engine, stream, server
```

`trafficmodel` is the deepest internal dependency. Keeping it import-free is what breaks the `memory ↔ traffic` cycle.

---

## Traffic modelling

Inspired by the OpenTraffic traffic-engine, but implemented in-memory and real-time.

### TripLine system

Virtual perpendicular lines are placed across road segments. Each segment gets an entry line (index 1) and an exit line (index 2). When a vehicle's GPS track crosses both in order, a speed sample is computed from the time delta and the known distance between the lines.

```
segment start ──── [entry TripLine] ──────────── [exit TripLine] ──── segment end
                         ↑                               ↑
                   crossing.Time = t1             crossing.Time = t2
                                    speed = dist / (t2 - t1)
```

Speed samples above 31 m/s (~111 km/h) are filtered as GPS artefacts.

### Speed histogram

Each `MemoryEntity` maintains a `SpeedHistogram` — a sparse `map[uint16]int64` packing `(hour_of_week × 120 + speed_bin)` into a single key. This gives 168 × 120 = 20,160 possible buckets covering a full week at 1 km/h resolution, with typical memory use under 10 KB per entity.

### Anomaly detection flow

```
entity.ProcessTrafficCrossing(crossing)
  ↓
SpeedSample computed (entry → exit pair matched)
  ↓
Sample added to histogram
  ↓
If speed > 1.5× expected OR speed < 0.5× expected:
    RiskScore  += 0.2
  ↓
If deviation > 2σ from segment mean:
    AnomalyReport broadcast to SSE subscribers
    Event persisted to SQLite anomalies table
```

---

## Traffic API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/traffic/segments/profile?segment_id=` | Speed histogram stats: mean, stddev, hourly averages |
| `GET` | `/traffic/state` | Snapshot: active vehicles, anomalies, congestion level |
| `GET` | `/traffic/coverage` | Observability: % observable / partial / dead zone |
| `POST` | `/traffic/crossings` | Inject an external TripLine crossing event |
| `GET` | `/traffic/stream/anomalies` | SSE stream of real-time anomaly alerts |
| `GET` | `/traffic/h3/state?lat=&lon=` | Traffic state for the H3 hexagon at a coordinate |
| `GET` | `/traffic/h3/boundary?lat=&lon=` | Hexagon boundary polygon for map rendering |
| `GET` | `/traffic/h3/neighbors?lat=&lon=` | Six adjacent hexagons for congestion propagation |

### Congestion levels

| Level | Threshold |
|-------|-----------|
| `LOW` | ≥ 25 m/s (~90 km/h) |
| `MEDIUM` | 15–25 m/s (54–90 km/h) |
| `HIGH` | < 15 m/s (~54 km/h) |
| `UNKNOWN` | No speed data |

---

## Data sources

| Source | Latency | Characteristic |
|--------|---------|---------------|
| Redis | sub-second | Live position stream, rolling window |
| ClickHouse | seconds–minutes | Historical batch, higher trust once stable |
| Merged | — | Reconciled output of both |

**Resolution order** when two sources have the same timestamp:
1. Newer `TimestampNS` wins outright.
2. A ClickHouse record older than `stabilityThreshold` (default 5 min) beats Redis.
3. Higher `IngestSeq` breaks remaining ties.

**Confidence scoring** blends recency decay with a per-source bonus:

| Source | Bonus |
|--------|-------|
| Redis | +0.15 |
| ClickHouse | +0.10 |
| Merged | +0.05 |

---

## API

### Health
```bash
curl http://localhost:8080/health
```

### Stats
```bash
curl http://localhost:8080/stats
# {
#   "clients": 5,
#   "memory_entities": 1234,
#   "memory_active": 987,
#   "memory_stale": 12,
#   "traffic_samples": 5678,
#   "anomalies_detected": 23
# }
```

### Subscribe
```bash
POST /subscribe?client_id=abc123
{
  "viewport": { "min_lat": -1.30, "max_lat": -1.28, "min_lon": 36.81, "max_lon": 36.84 },
  "focus_lat": -1.292,
  "focus_lon": 36.821,
  "preferences": { "anomaly_priority": true },
  "max_results": 300
}
```

### Stream
```javascript
const es = new EventSource("http://localhost:8080/stream?client_id=abc123");
es.onmessage = (event) => {
  const { vehicles } = JSON.parse(event.data);
  renderVehicles(vehicles);
};
```

### Anomaly stream
```javascript
const es = new EventSource("http://localhost:8080/traffic/stream/anomalies");
es.onmessage = (event) => {
  const anomaly = JSON.parse(event.data);
  console.log(anomaly.vehicle_id, "speed:", anomaly.speed_ms, "σ:", anomaly.deviation_std);
};
```

---

## Running

```bash
# Development
go run ./main.go

# Production build
go build -o hypntyz .
./hypntyz
```

### Configuration

`types.ClientConfig` controls the key tunables. `SirtebasinURL` and `RedisURL` have no defaults and must be set before the engine starts.

| Field | Default | Description |
|-------|---------|-------------|
| `TickRateHz` | 20 | Pipeline tick rate |
| `MaxVehiclesPerClient` | 500 | Top-K cap per SSE stream |
| `MaxClientsPerNode` | 10,000 | Concurrent SSE connections |
| `EnableBackpressure` | true | Drop slow clients rather than stall the pipeline |
| `RegionID` | `"default"` | Node identity for multi-region deployments |

Memory and decay tunables live in `memory.MemoryConfig`:

| Field | Default | Description |
|-------|---------|-------------|
| `MaxEntities` | 100,000 | Hard cap on tracked vehicles |
| `EntityTTL` | 30 min | Eviction threshold for unseen entities |
| `DecayInterval` | 1 s | How often the salience decay loop runs |
| `DecayRate` | 0.995 | Per-second salience multiplier |

---

## Tests

```bash
go test ./... -v

# ✓ Memory entity creation / apply / decay
# ✓ Store upsert / query / decay / GC
# ✓ Embedding generation and similarity
# ✓ Compiler determinism and feature dimensions
# ✓ Compiler timestamp ordering invariant
# ✓ Source encoding passthrough
# ✓ TripLine crossing detection
# ✓ Speed computation from crossing pair
# ✓ Histogram binning and statistics
# ✓ Speed deviation detection
```

---

## Persistence

SQLite (`traffic.db`) is created in the working directory on first run. WAL mode is enabled; the `-wal` and `-shm` sibling files are normal and safe to ignore.

| Table | Contents |
|-------|----------|
| `speed_samples` | Histogram bins keyed by `(segment_id, hour_of_week, speed_bin)` |
| `anomalies` | Timestamped events with vehicle, segment, speed, deviation, risk score |

---

## Architecture properties

| Property | Implementation |
|----------|----------------|
| Deterministic ordering | Timestamp dominance in merge engine |
| Replay safety | Windowing + watermark invariants |
| Memory persistence | Entity state evolves across ticks, not recomputed |
| Attention over memory | Scores entities, not raw vehicle events |
| Predictive cognition | Trajectory extrapolation + embedding evolution |
| Traffic modelling | TripLine crossings + per-segment speed histograms |
| Scalability | O(events × mutations), not O(clients × vehicles × ticks) |

---

## Deployment notes

- H3 spatial index requires CGO (`gcc` or `clang`). Cross-compile with `CGO_ENABLED=1`.
- H3 resolution 9 gives ~0.1 km² hexagons. Memory footprint is approximately 100 MB for 1 M vehicles at this resolution.
- SQLite is appropriate for single-node deployments. For multi-node, replace `Persistor` with a shared store (Postgres, TiKV, etc.).
- The Redis client in `sirtebasin/redis_client.go` is currently a stub. Wire in `github.com/redis/go-redis/v9` and implement `XRANGE`/`XREVRANGE` to activate real-time ingestion.

---

## Mental model

This is not a traffic streaming system.

This is a **persistent real-time cognition engine** that:

- Remembers vehicles across observations
- Accumulates anomaly evidence over time
- Learns trajectory patterns
- Maintains latent embeddings per entity
- Decays irrelevant memories
- Detects traffic anomalies in real-time
- Surfaces the cognitively most relevant entities to each client
