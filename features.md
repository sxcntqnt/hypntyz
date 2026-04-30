```md
# Projection Engine (Go + Attention Core)
## Sirtebasin Semantic Thinning System

This document defines the design for a **Production Projection Engine** that sits between Sirtebasin (truth/query layer) and downstream clients (SSE/WebSocket).

It uses a Go-based attention system to perform **real-time semantic thinning** of vehicle telemetry into *attention-weighted projections*.

---

# 1. System Purpose

The Projection Engine is responsible for:

- Converting raw Sirtebasin vehicle streams into **attention-ranked subsets**
- Reducing 100k+ concurrent vehicles into **client-relevant projections**
- Enforcing **attention budgets per client**
- Maintaining sub-200ms decision latency under load

It is NOT a database.
It is NOT a storage system.
It is a **real-time inference + filtering engine**.

---

# 2. Core Concept

## Data Flow

```

Sirtebasin (Query Engine)
↓
Vehicle Snapshot Stream (raw truth)
↓
Projection Engine (THIS SYSTEM)
↓
Attention Model (Go Attention Core)
↓
Scored Vehicle Set
↓
Policy Layer (client-aware thinning)
↓
SSE / WebSocket delivery

```

---

# 3. Architecture Overview

## 3.1 Modules

```

/projection-engine
├── cmd/
│     └── main.go
│
├── internal/
│     ├── engine/        # main projection loop
│     ├── attention/     # Go attention wrapper integration
│     ├── query/         # Sirtebasin query client
│     ├── cache/         # Redis hot-state cache
│     ├── policy/        # client filtering + budgets
│     ├── batching/      # tick engine (10–50Hz)
│     ├── backpressure/  # load shedding + rate control
│     ├── vector/        # zero-allocation vector ops
│     ├── region/        # sharding + geo partitioning
│     └── telemetry/     # metrics + tracing
│
├── pkg/
│     ├── types/         # shared domain models
│     ├── interfaces/    # engine contracts
│     └── config/
│
├── go.mod
└── README.md

```

---

# 4. Core Execution Model

## 4.1 Tick-Based Projection Loop

The engine runs a deterministic loop:

```

for every tick (10–50Hz):

```
1. Fetch active viewport requests
2. Pull vehicle deltas from Sirtebasin
3. Load cached embeddings (Redis)
4. Build query vectors per client
5. Run attention scoring
6. Apply policy thinning
7. Emit SSE batch updates
```

```

---

## 4.2 Tick Engine (Batching Layer)

### Requirements

- 10–50Hz configurable tick rate
- adaptive tick slowdown under load
- bounded queue processing
- deterministic execution window

### Behavior

```

tick(duration):
start_time := now()

```
process_all_clients()

if overload:
    reduce_tick_rate()
    enable_backpressure()

sleep_until(next_tick_window)
```

```

---

# 5. Attention Integration Layer

## 5.1 Role of Go Attention Engine

The attention module is used as:

> A **relevance scorer**, not a neural network trainer.

### Input

For each vehicle:

```

query vector = client intent vector
key vector   = vehicle state vector
value vector = enriched telemetry

```

### Output

```

attention_score + weighted vehicle embedding

```

---

## 5.2 Feature Construction (Zero Allocation Path)

Each vehicle is transformed into:

```

struct FeatureVector {
position_h3      uint64
velocity         float32
heading          float32
anomaly_score    float32
fleet_id         uint32
temporal_decay   float32
}

```

Rules:

- NO heap allocation in hot path
- pooled vector reuse only
- fixed-size structs for SIMD compatibility

---

## 5.3 Scoring Pipeline

```

vehicle → feature vector
→ attention.DotProduct(query, key)
→ score
→ ranking buffer

```

---

# 6. Redis Cache Layer

## Purpose

Reduce repeated Sirtebasin queries and store:

- recent vehicle states (5–15s window)
- precomputed embeddings
- client attention vectors

## Keys

```

vehicle:{id}              → latest state
embedding:{vehicle_id}    → precomputed vector
client:{id}:state         → viewport + policy
region:{h3}              → vehicle set

```

---

## Eviction Policy

- TTL-based (5–15 seconds)
- region-based invalidation
- write-through from Sirtebasin stream

---

# 7. Backpressure Control System

## Purpose

Prevent overload collapse during:

- traffic spikes
- client explosion
- region hot zones

## Mechanisms

### 7.1 Hard Limits

```

max_vehicles_per_tick = 100k
max_clients_per_node  = 10k
max_attention_ops/sec = bounded

```

---

### 7.2 Soft Degradation

When overloaded:

1. Reduce tick rate (50Hz → 10Hz)
2. Reduce projection depth
3. Drop low-score vehicles
4. Switch to cluster-level output

---

### 7.3 Load Shedding Priority

```

1. anomaly vehicles (always kept)
2. viewport vehicles
3. near-focus vehicles
4. background traffic (drop first)

```

---

# 8. Vector System (Performance Layer)

## Goals

- zero allocation per tick
- SIMD-friendly memory layout
- pooled vector reuse
- cache-line alignment

## Implementation

```

type VectorPool struct {
pool sync.Pool
}

```

### Rules

- never allocate in scoring loop
- reuse buffers per tick
- pre-sized matrices only
- flatten all vectors to contiguous arrays

---

# 9. Distributed Design

## 9.1 Region Sharding

System is partitioned by spatial regions:

```

Region A (Nairobi West)
Region B (CBD)
Region C (Eastlands)
...

```

Each Projection Engine instance owns:

- vehicle subset
- client subset
- local cache segment

---

## 9.2 Sirtebasin Fanout Strategy

Sirtebasin emits:

```

vehicle updates → region router → projection engine shards

```

Routing logic:

```

h3_index(vehicle) → shard_id → engine instance

````

---

## 9.3 Cross-Region Queries

Handled via:

- lightweight cross-shard fetch
- cached ghost vehicles (edge overlap zones)
- eventual consistency only

---

# 10. Engine Interfaces

## 10.1 Core Projection Interface

```go
type ProjectionEngine interface {
    Tick(ctx context.Context) error
    ProcessClient(client ClientState) ([]Projection, error)
    Score(vehicle Vehicle, client ClientState) float64
}
````

---

## 10.2 Sirtebasin Adapter

```go
type SirtebasinClient interface {
    FetchDelta(region string, since int64) ([]Vehicle, error)
}
```

---

## 10.3 Attention Wrapper

```go
type AttentionCore interface {
    Score(query Vector, key Vector) float64
    BatchScore(q []Vector, k []Vector) []float64
}
```

---

# 11. Performance Targets

| Metric              | Target                 |
| ------------------- | ---------------------- |
| Tick latency        | < 20ms                 |
| Per vehicle scoring | < 200ns                |
| Memory allocation   | ~0 in hot path         |
| SSE batch build     | < 5ms                  |
| Max throughput      | 100k vehicles/sec/node |

---

# 12. System Behavior Summary

The Projection Engine acts as:

> A real-time attention compiler over geospatial truth.

It does NOT:

* store truth
* query long-term history
* perform analytics joins

It DOES:

* compute relevance
* reduce data entropy
* enforce attention budgets
* shape reality per client

---

# 13. Final Mental Model

```
Sirtebasin = Truth Layer (everything that exists)
Projection Engine = Attention Layer (what matters)
Client = Perception Layer (what is shown)
```

---

# 14. Extension Roadmap

### Phase 1

* tick engine
* attention scoring integration
* Redis cache

### Phase 2

* SIMD vectorization
* zero allocation pipeline
* backpressure control

### Phase 3

* regional sharding
* multi-node fanout
* cross-shard ghosting

---

# 15. Summary

This system is not a pipeline.

It is a **real-time attention allocation machine** built on top of geospatial truth.

The Go attention engine is not the product.

It is the **scoring substrate for perception itself**.

```
```
