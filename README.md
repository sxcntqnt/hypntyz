# Projection Engine - Cognitive Realtime System

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
│              MEMORY ENGINE (NEW!)                           │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  MemoryEntity with persistent state:                 │  │
│  │  • Position, velocity, trajectory                    │  │
│  │  • Salience, risk score, anomaly count               │  │
│  │  • Embedding (latent representation)                 │  │
│  │  • Attention history                                 │  │
│  │  • Predictive state                                  │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  Operations:                                                │
│  • Upsert(event) → evolve entity state                     │
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
│    • Anomaly boost                                          │
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

## Key Innovation: Memory System

### Before (Stateless)
```
event → score → emit → discard
```
Every tick recomputes from scratch. No continuity.

### After (Cognitive)
```
event → memory.update() → attention.query() → emit
            ↓
    Persistent entity state
    • Trajectory tracking
    • Salience accumulation
    • Anomaly persistence
    • Embedding evolution
```

## Memory Entity Structure

```go
type MemoryEntity struct {
    ID string
    
    // Current state
    Position  Position
    Velocity  Velocity
    
    // Temporal memory
    Trajectory    []Position
    LastSeen      time.Time
    SeenCount     int
    
    // Attention
    Salience       float64
    RiskScore      float64
    AnomalyCount   int
    AttentionHistory []float64
    
    // Semantic
    Embedding Vector
    Classification string
    
    // Predictive
    PredictedPath []Position
}
```

## Configuration

```bash
# Memory settings
MAX_MEMORY_ENTITIES=100000
MEMORY_ENTITY_TTL=30m

# Decay settings
DECAY_RATE=0.995
DECAY_INTERVAL=1s
```

## API

### Health
```bash
curl http://localhost:8080/health
```

### Stats (includes memory metrics)
```bash
curl http://localhost:8080/stats
# {
#   "clients": 5,
#   "memory_entities": 1234,
#   "memory_active": 987,
#   "memory_stale": 12,
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
```

## Architecture Properties

| Property | Implementation |
|----------|----------------|
| **Deterministic ordering** | Timestamp dominance in merge engine |
| **Replay safety** | Windowing + watermark invariants |
| **Memory persistence** | Entity state evolves, not recomputed |
| **Attention over memory** | Scores entities, not raw vehicles |
| **Predictive cognition** | Trajectory + embedding evolution |
| **Scalability** | O(events × mutations) not O(clients × vehicles × ticks) |

## Mental Model

This is no longer a **traffic streaming system**.

This is a **persistent realtime cognition engine** that:
- Remembers vehicles across observations
- Accumulates anomaly evidence
- Learns trajectory patterns
- Maintains latent embeddings
- Decays irrelevant memories
- Surfaces cognitively relevant entities

The system now has **memory**, **attention**, and the beginnings of **predictive cognition**.
