# hypntyz

A real-time vehicle projection engine with persistent cognitive memory, traffic modelling, and frequency-domain behavioural analysis. hypntyz ingests high-frequency GPS telemetry, runs it through a stateful cognitive pipeline enriched with FFT-based spectral features, and streams scored, ranked vehicle projections to connected clients over Server-Sent Events. It is designed to handle tens of thousands of concurrent clients and hundreds of thousands of tracked vehicles on a single node.

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
│              SPECTRAL FEATURE COMPILER          (NEW)       │
│  - TensorSequence + speed history → EnrichedSequence        │
│  - Hann-windowed FFT on speed signal (N=32, 20 Hz)          │
│  - Band energies: low / mid / high                          │
│  - Dominant frequency, spectral entropy, coherence          │
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
│  │  • Speed sample + SpectralDeviationScore             │  │
│  │  • Per-segment speed histograms                      │  │
│  │  • Anomaly detection via speed deviation             │  │
│  │                                                      │  │
│  │  SPECTRAL COGNITION                     (NEW)        │  │
│  │  • Speed ring buffer (32 samples)                    │  │
│  │  • SpectralProfile: route signature FFT              │  │
│  │  • Entropy spike detection                           │  │
│  │  • Spectral anomaly score [0, 1]                     │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  Operations:                                                │
│  • Upsert(event)              → evolve entity + FFT        │
│  • ProcessTrafficCrossing()   → speed + spectral deviation │
│  • Decay()                    → gradual salience fade      │
│  • Query(client)              → retrieve relevant entities │
│  • GarbageCollect()           → remove stale entities      │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              ATTENTION ENGINE                               │
│  - Scores MemoryEntities (not raw vehicles)                 │
│  - Combines:                                                │
│    • Geometric relevance    (50%)                           │
│    • Entity salience        (30%)                           │
│    • Spectral anomaly score (20%)       (NEW)               │
│    • Discrete anomaly boost (+0.15)                         │
│  - Records attention history per entity                     │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              RANKER + THINNING                              │
│  - Anomaly flag promotion (always surfaces first)           │
│  - Sort by combined score                                   │
│  - Top-K selection                                          │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              SSE BROADCAST                                  │
│  { "client_id": "abc", "vehicles": [...] }                  │
└─────────────────────────────────────────────────────────────┘
```

---

## Key innovation: memory + traffic + spectral cognition

### Before (stateless)

```
event → score → emit → discard
```

Every tick recomputes from scratch. No continuity between observations.

### After (cognitive + traffic + spectral)

```
event → memory.Upsert() → spectral.FFT() → traffic.Crossing() → attention.Score() → emit
              ↓                  ↓                  ↓
   Persistent entity      Frequency domain    Speed samples
   Trajectory tracking    Entropy history     Histogram aggregation
   Salience accumulation  Route signature     Spectral deviation score
   Embedding evolution    Anomaly score       Risk score propagation
```

The engine now has **memory**, **attention**, **traffic-aware cognition**, and **frequency-domain behavioural fingerprinting**.

---

## Package layout

```
internal/
├── types/              Shared wire types and constants
│   ├── types.go        Viewport, ClientState, Vehicle, Projection, FeatureVector
│   └── vehicle_state.go  VehicleState, TensorSequence, QueryRequest, SourceType
│
├── trafficmodel/       Pure traffic data types — no internal imports
│   ├── tripline.go     TripLine, Crossing, SpeedSample (+ SpectralDeviationScore)
│   └── histogram.go    SpeedHistogram (168 h × 120 speed bins)
│
├── spectral/           Frequency-domain signal processing — no internal imports
│   ├── ring_buffer.go  Fixed-size circular speed sample buffer
│   ├── fft.go          Hann-windowed real FFT (gonum/dsp/fourier)
│   ├── features.go     SpectralFeatures, SpectralProfile, EnrichedSequence
│   └── compiler.go     SpectralFeatureCompiler: TensorSequence → EnrichedSequence
│
├── memory/             In-process entity store
│   ├── entity.go       MemoryEntity: kinematics, salience, traffic + spectral model
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
│   ├── persistence.go  SQLite: speed_samples + anomalies + spectral_signatures
│   └── spatial_index.go  H3 spatial index (resolution 9, ~0.1 km²)
│
├── attention/          Attention scoring engine (spectral-aware)
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
        ├── trafficmodel     ← zero internal imports
        └── spectral         ← zero internal imports
              └── memory     ← imports trafficmodel + spectral
                    └── traffic  ← attention, ranker, engine, stream, server
```

Both `trafficmodel` and `spectral` sit at the base of the graph with no internal imports. This is what allows `memory` to import both without creating cycles.

---

## Spectral feature pipeline

### Why FFT on vehicle speed?

Raw speed and position tell you *what* a vehicle is doing right now. The FFT of the speed signal over time tells you *how it's behaving* — whether it's driving smoothly, oscillating (stop-start traffic), or erratically (hard braking, GPS jitter, evasive manoeuvres). This distinction is invisible to point-in-time scoring but obvious in the frequency domain.

### How it works

Each `MemoryEntity` maintains a `RingBuffer` of the last 32 speed samples (1.6 seconds at 20 Hz). On every `Apply()` call — once the buffer holds at least 16 samples — a Hann-windowed FFT is computed and the result is stored in `SpectralProfile`:

```
speed ring buffer (32 samples × 20 Hz = 1.6 s window)
    ↓  Hann window (suppress spectral leakage)
    ↓  Real FFT → 17 magnitude bins
    ↓  Band energy extraction
         Low  (bins 1–4):   slow trends, gradual accel/decel
         Mid  (bins 5–8):   traffic-signal-scale oscillations
         High (bins 9–16):  rapid fluctuations, jitter, hard braking
    ↓  Spectral entropy (normalised Shannon, [0,1])
         Near 0 → one dominant frequency (periodic, predictable)
         Near 1 → energy spread uniformly (erratic, unpredictable)
    ↓  Coherence score: dominant bin energy / total energy
    ↓  AnomalyScore = 0.5 × HighBandEnergy + 0.5 × SpectralEntropy
```

The `SpectralFeatureCompiler` additionally merges the entity's historical ring buffer with the current `TensorSequence` speed tokens, giving a cross-tick view before the memory upsert.

### Spectral deviation on crossings

When a TripLine crossing generates a `SpeedSample`, the memory layer immediately runs an FFT on the current ring buffer and writes the result to `SpeedSample.SpectralDeviationScore`. This enriches the crossing event with frequency-domain context at the exact moment it was detected — independent of the tick-level profile update.

```
ProcessTrafficCrossing(crossing)
  ↓
SpeedSample computed (entry → exit pair matched)
  ↓
FFT on current ring buffer → SpectralDeviationScore written to sample
  ↓
If SpectralDeviationScore > 0.6 → RiskScore += 0.1
  ↓
If speed > 1.5× expected OR < 0.5× expected → RiskScore += 0.2
  ↓
If deviation > 2σ from segment mean:
    AnomalyReport broadcast to SSE subscribers + persisted
```

### Entropy spike detection

`SpectralProfile.EntropyHistory` keeps a rolling window of the last 10 entropy values. On each `Apply()`, if the latest entropy exceeds the rolling mean by more than 2 standard deviations, `Salience` is boosted by 0.15. This surfaces vehicles with suddenly erratic behaviour faster than the discrete anomaly counter would.

---

## Attention scoring

The attention engine blends three continuous signals and one discrete boost:

```
score = geoScore × 0.50
      + entity.Salience × 0.30
      + entity.SpectralProfile.AnomalyScore × 0.20
      + 0.15  (if entity.IsAnomalous())
```

| Component | Weight | Source |
|-----------|--------|--------|
| Geometric relevance | 50% | Dot-product attention on entity embedding vs client focus |
| Entity salience | 30% | Accumulated from anomaly events + exponential decay |
| Spectral anomaly score | 20% | FFT-derived: high-frequency energy + entropy |
| Discrete anomaly boost | +0.15 | Fires when AnomalyCount > 2 or RiskScore > 0.7 |

---

## Traffic modelling

Inspired by the OpenTraffic traffic-engine, implemented in-memory and real-time.

### TripLine system

Virtual perpendicular lines across road segments. Entry (index 1) and exit (index 2) crossings pair to produce a `SpeedSample`. Each sample now carries a `SpectralDeviationScore` populated by the memory layer.

```
segment start ──── [entry TripLine] ──────── [exit TripLine] ──── segment end
                         ↑                          ↑
                   crossing.Time = t1         crossing.Time = t2
                          speed = dist / (t2 - t1)
                          SpectralDeviationScore = FFT(ring buffer)
```

Speed samples above 31 m/s (~111 km/h) are filtered as GPS artefacts.

### Speed histogram

Each `MemoryEntity` maintains a `SpeedHistogram` — a sparse `map[uint16]int64` packing `(hour_of_week × 120 + speed_bin)` into a single key. 168 × 120 = 20,160 buckets, covering a full week at 1 km/h resolution, typically under 10 KB per entity.

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
|--------|---------|----------------|
| Redis | sub-second | Live position stream, rolling window |
| ClickHouse | seconds–minutes | Historical batch, higher trust once stable |
| Merged | — | Reconciled output of both |

**Resolution order** when two sources have the same timestamp:
1. Newer `TimestampNS` wins outright.
2. A ClickHouse record older than `stabilityThreshold` (default 5 min) beats Redis.
3. Higher `IngestSeq` breaks remaining ties.

---

## API

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

`EngineConfig` controls the pipeline tunables. `SirtebasinURL`, `RedisURL`, and `ClickHouseHost` must be set before the engine starts; they have no defaults.

| Field | Default | Description |
|-------|---------|-------------|
| `TickRateHz` | 20 | Pipeline tick rate |
| `MaxVehiclesPerClient` | 500 | Top-K cap per SSE stream |
| `MaxClientsPerNode` | 10,000 | Concurrent SSE connections |
| `EnableBackpressure` | true | Drop slow clients rather than stall the pipeline |
| `RegionID` | `"default"` | Node identity for multi-region deployments |
| `WindowSizeNS` | 60 s | Sliding window duration |
| `SlideNS` | 30 s | Slide interval |
| `AllowedLatenessNS` | 5 s | Late-event tolerance before dead-lettering |

Memory and decay tunables (`memory.MemoryConfig`):

| Field | Default | Description |
|-------|---------|-------------|
| `MaxEntities` | 100,000 | Hard cap on tracked vehicles |
| `EntityTTL` | 30 min | Eviction threshold for unseen entities |
| `DecayInterval` | 1 s | Salience decay loop interval |
| `DecayRate` | 0.995 | Per-second salience multiplier |

Spectral tunables (`spectral` package constants):

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultWindowSize` | 32 | FFT frame size (power of 2) |
| `DefaultMinSamples` | 16 | Minimum samples before FFT runs |
| `DefaultSampleRate` | 20.0 Hz | Assumed sampling rate (matches tick rate) |

---

## Tests

```bash
go get gonum.org/v1/gonum@latest   # required for spectral package
go test ./... -v

# ✓ Ring buffer wraparound and chronological ordering
# ✓ FFT DC component, single-frequency peak, Nyquist axis
# ✓ Band energies sum to 1.0
# ✓ Spectral entropy bounded [0, 1]
# ✓ Higher entropy for irregular vs sine-wave signal
# ✓ Entropy spike detection via rolling z-score
# ✓ Spectral compiler: invalid below MinSamples
# ✓ Spectral compiler: history merge across ticks
# ✓ Memory entity spectral fields initialised
# ✓ SpectralProfile updated after MinSamples threshold
# ✓ SpeedSample SpectralDeviationScore defaults to zero
# ✓ IsSpectrallyAnomalous threshold logic
# ✓ Compiler determinism and feature dimensions
# ✓ TripLine crossing detection and speed computation
# ✓ Histogram binning and statistics
# ✓ Watermark monotonicity
# ✓ Late event rejection and dead-letter queue
# ✓ Window stats
```

---

## Persistence

SQLite (`traffic.db`) is created in the working directory on first run. WAL mode is enabled; the `-wal` and `-shm` sibling files are normal and safe to ignore.

| Table | Contents |
|-------|----------|
| `speed_samples` | Histogram bins keyed by `(segment_id, hour_of_week, speed_bin)` |
| `anomalies` | Timestamped crossing anomaly events with speed, deviation, risk score |
| `spectral_signatures` | Per-vehicle FFT fingerprint: band energies, entropy, route signature blob |

The `spectral_signatures` table uses `INSERT OR REPLACE` with `UNIQUE(vehicle_id)` — one current row per vehicle. The route signature is stored as a little-endian float32 blob (68 bytes at default window size). `EntropyHistory` is not persisted; it rebuilds within seconds from live observations.

`GetHighAnomalyVehicles` queries this table on restart to immediately bootstrap the attention engine with historically anomalous vehicles, without waiting for their ring buffers to refill.

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
| Spectral cognition | Per-entity FFT profile, entropy spike detection |
| Scalability | O(events × mutations), not O(clients × vehicles × ticks) |
| No import cycles | trafficmodel and spectral have zero internal imports |

---

## Deployment notes

- H3 spatial index requires CGO (`gcc` or `clang`). Cross-compile with `CGO_ENABLED=1`.
- H3 resolution 9 gives ~0.1 km² hexagons. Memory footprint ~100 MB for 1 M vehicles.
- The spectral ring buffer adds 32 × 8 = 256 bytes per entity. At 100,000 entities that is 25 MB — negligible.
- SQLite is appropriate for single-node deployments. For multi-node, replace `Persistor` with a shared store (Postgres, TiKV, etc.).
- The Redis client in `sirtebasin/redis_client.go` is currently a stub. Wire in `github.com/redis/go-redis/v9` and implement `XRANGE`/`XREVRANGE` to activate real-time ingestion.
- Delete `internal/traffic/tripline.go` — its types (`Point`, `TripLine`, `Crossing`, `SpeedSample`) were moved to `internal/trafficmodel` to break the import cycle. The file is dead code and should be removed.

---

## Mental model

This is not a traffic streaming system.

This is a **persistent real-time cognition engine** that:

- Remembers vehicles across observations
- Accumulates anomaly evidence over time
- Learns trajectory patterns in the frequency domain
- Maintains latent embeddings per entity
- Decays irrelevant memories
- Detects traffic anomalies in real-time via both statistical deviation and spectral analysis
- Surfaces the cognitively and spectrally most relevant entities to each client
