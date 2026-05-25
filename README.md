# Projection Engine - Cognitive Realtime System with Traffic Modeling

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    CLIENT LAYER                             │
│  POST /subscribe  →  Client viewport + preferences          │
│  GET  /stream     →  SSE real-time updates                  │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                  TICK LOOP (20Hz)                           │
│  - Runs every 50ms                                          │
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
│           MEMORY ENGINE + TRAFFIC MODELING                  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  MemoryEntity with persistent state:                 │  │
│  │  • Position, velocity, trajectory                    │  │
│  │  • Salience, risk score, anomaly count               │  │
│  │  • Embedding (latent representation)                 │  │
│  │  • Attention history                                 │  │
│  │  • Predictive state                                  │  │
│  │                                                      │  │
│  │  TRAFFIC MODELING (NEW!)                             │  │
│  │  • TripLine crossing detection                       │  │
│  │  • Speed sample generation                           │  │
│  │  • Per-segment speed histograms                      │  │
│  │  • Anomaly detection via speed deviation             │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  Operations:                                                │
│  • Upsert(event) → evolve entity state                     │
│  • ProcessTrafficCrossing() → detect speed anomalies       │
│  • Decay() → gradual salience fade                         │
│  • Query(client) → retrieve relevant entities              │
│  • GarbageCollect() → remove stale entities                │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              ATTENTION ENGINE                               │
│  - Scores MemoryEntities (not raw vehicles!)                │
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

## Key Innovation: Memory + Traffic System

### Before (Stateless)
```
event → score → emit → discard
```
Every tick recomputes from scratch. No continuity.

### After (Cognitive + Traffic Modeling)
```
event → memory.update() → traffic.crossing() → attention.query() → emit
            ↓                      ↓
    Persistent entity      Speed samples
    Trajectory tracking    Histogram aggregation
    Salience accumulation  Anomaly detection
    Embedding evolution    Risk score boost
```

## Traffic Modeling Details

Inspired by the OpenTraffic traffic-engine (Java), but implemented **in-memory** and **real-time**:

### TripLine System
- Virtual perpendicular lines across road segments
- Entry (index=1) and Exit (index=2) pairs
- Line-intersection math for crossing detection
- Speed computed from crossing time delta

### Speed Histogram
- Bucketed by hour-of-week (0-167) and speed bin (0-120 km/h)
- Compact `uint16` packing: `bin = hour * 120 + speed_bin`
- Running statistics: count, mean, stddev
- Anomaly detection: deviation from expected speed

### MemoryEntity Integration
```go
entity.ProcessTrafficCrossing(crossing)
  ↓
SpeedSample generated
  ↓
Added to histogram
  ↓
If speed > 1.5x expected OR < 0.5x expected:
  RiskScore += 0.2
  Salience += 0.1
  ↓
Attention score boosted
```

## Configuration

```bash
# Memory settings
MAX_MEMORY_ENTITIES=100000
MEMORY_ENTITY_TTL=30m

# Traffic modeling
MAX_SPEED=31.0 m/s  # Filter unrealistic speeds
MIN_SEGMENT_LEN=60m  # Minimum segment length for trip lines

# Decay settings
DECAY_RATE=0.995
DECAY_INTERVAL=1s
```

## API

### Health
```bash
curl http://localhost:8080/health
```

### Stats (includes memory + traffic metrics)
```bash
curl http://localhost:8080/stats
# {
#   "clients": 5,
#   "memory_entities": 1234,
#   "memory_active": 987,
#   "memory_stale": 12,
#   "traffic_samples": 5678,
#   "anomalies_detected": 23,
#   ...
# }
```

### Subscribe
```bash
POST /subscribe?client_id=abc123
{
  "viewport": { "min_lat": ..., "max_lat": ..., ... },
  "focus": { "lat": ..., "lon": ... },
  "preferences": { "anomaly_priority": true },
  "max_results": 300
}
```

### Stream
```javascript
const es = new EventSource("http://localhost:8080/stream?client_id=abc123");
es.onmessage = (event) => {
  const data = JSON.parse(event.data);
  renderVehicles(data.vehicles);
};
```

## Running

```bash
# Development
go run ./main.go

# Production
go build -o projection-engine .
./projection-engine
```

## Tests

```bash
go test ./... -v
# ✓ Memory entity creation/apply/decay
# ✓ Store upsert/query/decay
# ✓ Embedding generation
# ✓ Garbage collection
# ✓ Window determinism
# ✓ Compiler invariants
# ✓ Traffic trip line crossing
# ✓ Speed histogram binning
# ✓ Speed deviation detection
```

## Architecture Properties

| Property | Implementation |
|----------|----------------|
| **Deterministic ordering** | Timestamp dominance in merge engine |
| **Replay safety** | Windowing + watermark invariants |
| **Memory persistence** | Entity state evolves, not recomputed |
| **Attention over memory** | Scores entities, not raw vehicles |
| **Predictive cognition** | Trajectory + embedding evolution |
| **Traffic modeling** | TripLine crossing + speed histograms |
| **Scalability** | O(events × mutations) not O(clients × vehicles × ticks) |

## Mental Model

This is no longer a **traffic streaming system**.

This is a **persistent realtime cognition engine** that:
- Remembers vehicles across observations
- Accumulates anomaly evidence
- Learns trajectory patterns
- Maintains latent embeddings
- Decays irrelevant memories
- **Detects traffic anomalies in real-time**
- Surfaces cognitively relevant entities

The system now has **memory**, **attention**, and **traffic-aware cognition**.
