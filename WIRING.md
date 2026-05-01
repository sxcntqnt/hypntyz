# Projection Engine - System Wiring Guide

## Complete Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLIENT REQUESTS                         │
│  POST /subscribe  →  Client viewport + preferences              │
│  GET  /stream     →  SSE stream for real-time updates           │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                         TICK LOOP (20Hz)                        │
│  - Runs every 50ms (configurable)                               │
│  - Fetches active clients                                       │
│  - Triggers projection pipeline                                 │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                    SIRTEBASIN QUERY ADAPTER                     │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Query Modes:                                            │   │
│  │  • RealtimeOnly    → Redis only (fast path)              │   │
│  │  • HistoricalOnly  → ClickHouse only                     │   │
│  │  • Hybrid          → Redis + ClickHouse merge            │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Merge Engine:                                                  │
│  • Timestamp dominance (newer wins)                             │
│  • Tie-break: ClickHouse > Redis (when stable)                  │
│  • Confidence scoring: [0,1] bounded                            │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                   WINDOWING POLICY ENGINE                       │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  StreamEvent → Window Assignment                         │   │
│  │  - Fixed/adaptive time ranges                            │   │
│  │  - Sliding windows (overlap support)                     │   │
│  │  - Watermarking for late events                          │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Guarantees:                                                    │
│  ✓ Deterministic segmentation                                   │
│  ✓ Replay safety                                                │
│  ✓ Watermark monotonicity                                       │
│  ✓ Finalized window immutability                                │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│               TEMPORAL SEQUENCE COMPILER                        │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  VehicleState[] → TensorSequence                         │   │
│  │  Feature vector: [lat, lon, speed, heading,              │   │
│  │                   confidence, time_delta, source]        │   │
│  │  F = 7 dimensions                                         │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Invariants:                                                    │
│  • Timestamp ordering preserved                                 │
│  • Confidence passthrough                                       │
│  • No reordering allowed                                        │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                  GO-ATTENTION ENGINE                            │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Multi-Head Self-Attention                               │   │
│  │  - Query: Client intent vector                           │   │
│  │  - Key:   Vehicle feature vector                        │   │
│  │  - Value: Enriched telemetry                            │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Output: Attention scores [0,1] per vehicle                     │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                    RANKER + THINNING                            │
│  • Sort by score (descending)                                   │
│  • Anomaly vehicles prioritized                                 │
│  • Top-K selection (max_results per client)                     │
│  • Viewport filtering                                           │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                      SSE BROADCAST                              │
│  {                                                              │
│    "client_id": "abc123",                                       │
│    "timestamp": 1720000001,                                     │
│    "vehicles": [                                                │
│      {"id": "KAA123B", "lat": -1.29, "lon": 36.82, "score": 0.94}
│    ]                                                            │
│  }                                                              │
└─────────────────────────────────────────────────────────────────┘
```

## Package Dependencies

```
main.go
  └─ internal/server/http.go
       ├─ internal/engine/engine.go (NEW - orchestrates full pipeline)
       │    ├─ internal/sirtebasin/adapter.go
       │    │    ├─ redis_client.go
       │    │    └─ clickhouse_client.go
       │    ├─ internal/window/window.go (NEW)
       │    ├─ internal/compiler/temporal_compiler.go
       │    ├─ internal/attention/engine.go
       │    └─ internal/ranker/ranker.go
       ├─ internal/stream/hub.go
       └─ internal/features/builder.go
```

## Key Integration Points

### 1. Server Initialization (`server/http.go:NewServer`)
```go
- Creates ProjectionEngine with config
- Initializes WindowingEngine with policy
- Sets up Attention scoring
- Creates SSE hub
```

### 2. Tick Loop (`server/http.go:tick`)
```go
- Fetches active clients
- Calls ProjectionEngine.ProcessTick()
- Broadcasts results via SSE
```

### 3. Projection Pipeline (`engine/engine.go:ProcessTick`)
```go
1. Query Sirtebasin Adapter (Hybrid mode)
2. Ingest states into Windowing Engine
3. Advance watermark
4. Emit finalized windows
5. Compile each window to tensor sequence
6. Score with attention
7. Rank and thin
8. Return projections
```

## Configuration Flow

```
Environment Variables
    ↓
types.ClientConfig (loadConfig)
    ↓
engine.EngineConfig
    ↓
├─ sirtebasin.AdapterConfig
├─ window.WindowPolicy
├─ attention.AttentionConfig
└─ ranker.Ranker config
```

## Testing Strategy

| Layer | Test File | Coverage |
|-------|-----------|----------|
| Sirtebasin Adapter | `adapter_test.go` | ✓ Timestamp monotonicity<br>✓ No duplicates<br>✓ Confidence bounds<br>✓ Merge idempotency |
| Windowing Engine | `window_test.go` | ✓ Deterministic windows<br>✓ No duplicates<br>✓ Watermark monotonicity<br>✓ Finalized immutability |
| Temporal Compiler | `compiler_test.go` | ✓ Determinism<br>✓ No reordering<br>✓ Fixed feature dim<br>✓ Confidence passthrough |

## Running the System

```bash
# Development
go run ./main.go

# With environment variables
SIRTEBASIN_URL=http://localhost:8080 \
REDIS_URL=redis://localhost:6379 \
CLICKHOUSE_HOST=localhost:9000 \
TICK_RATE_HZ=20 \
go run ./main.go

# Production build
go build -o projection-engine .
./projection-engine
```

## Monitoring

```bash
# Health check
curl http://localhost:8080/health

# Stats
curl http://localhost: