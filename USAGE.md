```md
# Projection Engine (Go + Attention Core)
## Sirtebasin Semantic Thinning Runtime

This is the **runtime guide** for deploying and using the Projection Engine — a high-throughput Go service that converts Sirtebasin vehicle truth streams into **attention-ranked, client-specific projections**.

It sits between:

- Sirtebasin (truth + query layer)
- SSE / WebSocket clients (UI layer)

and performs real-time **semantic thinning** using a Go-based attention engine.

---

# 1. What This System Does

The Projection Engine:

- Subscribes to Sirtebasin vehicle deltas
- Builds per-client query vectors
- Scores vehicles using Go Attention
- Filters and ranks results per client
- Emits only **relevant vehicles**, not raw streams

Instead of sending 100,000 vehicles:

> it sends ~50–500 meaningful ones per client.

---

# 2. Core Concept

```

Sirtebasin → Vehicle Stream → Projection Engine → Attention Scoring → Client View

````

You are not consuming raw telemetry.

You are consuming a **filtered perception layer**.

---

# 3. Prerequisites

## Required

- Go 1.21+
- Redis (for caching + state)
- Sirtebasin running (query + stream API)
- Optional: Jaeger (tracing)

---

# 4. Installation

```bash
git clone https://github.com/your-org/projection-engine
cd projection-engine
go mod tidy
````

---

# 5. Configuration

Create `.env`:

```env
SIRTEBASIN_URL=http://localhost:8080
REDIS_URL=redis://localhost:6379

TICK_RATE_HZ=20

MAX_VEHICLES_PER_CLIENT=500
MAX_CLIENTS_PER_NODE=10000

ENABLE_BACKPRESSURE=true
REGION_ID=nairobi-east
```

---

# 6. Running the Engine

## Development Mode

```bash
go run ./cmd/main.go
```

## Production Build

```bash
go build -o projection-engine ./cmd/main.go
./projection-engine
```

---

# 7. System Behavior

Once running:

### Step 1 — Tick Loop Starts

The engine runs a deterministic loop:

```
every 50ms (default 20Hz):

    pull client states
    fetch vehicle deltas
    build query vectors
    score vehicles
    apply thinning rules
    emit SSE batches
```

---

### Step 2 — Sirtebasin Stream Input

The engine listens for vehicle updates:

```json
{
  "vehicle_id": "KAA123B",
  "lat": -1.2921,
  "lon": 36.8219,
  "speed": 72,
  "heading": 180,
  "timestamp": 1720000000
}
```

---

### Step 3 — Attention Scoring

Each vehicle is converted into a feature vector:

```
[ position, velocity, heading, anomaly_score, fleet_id ]
```

Then scored against each client query:

```go
score := attention.Score(clientQuery, vehicleKey)
```

---

### Step 4 — Projection Output

Only top-scoring vehicles are sent:

```json
{
  "client_id": "abc123",
  "timestamp": 1720000001,
  "vehicles": [
    {
      "id": "KAA123B",
      "lat": -1.29,
      "lon": 36.82,
      "score": 0.94
    }
  ]
}
```

---

# 8. Client Connection (SSE)

Clients connect via:

```
GET /stream
```

## Example

```javascript
const es = new EventSource("http://localhost:9000/stream");

es.onmessage = (event) => {
  const data = JSON.parse(event.data);
  renderVehicles(data.vehicles);
};
```

---

# 9. Client Subscription Model

Clients send initial context:

```json
POST /subscribe
{
  "viewport": {
    "min_lat": -1.30,
    "max_lat": -1.25,
    "min_lon": 36.80,
    "max_lon": 36.85
  },
  "focus": { "lat": -1.292, "lon": 36.821 },
  "preferences": {
    "vehicle_types": ["matatu", "bus"],
    "anomaly_priority": true
  },
  "max_results": 300
}
```

This defines the **attention budget**.

---

# 10. Redis State (Internal)

The engine uses Redis for:

* vehicle state caching
* client session state
* precomputed embeddings

### Keys

```
vehicle:{id}        → latest state
client:{id}         → viewport + preferences
region:{h3}         → spatial grouping
embedding:{id}      → vector cache
```

---

# 11. Tick Engine Behavior

## Default Rate

```
20Hz (50ms ticks)
```

## Adaptive Scaling

| Condition | Behavior                |
| --------- | ----------------------- |
| Low load  | Increase tick precision |
| High load | Reduce tick rate        |
| Overload  | Drop low-score vehicles |

---

# 12. Backpressure System

When system overload occurs:

### Step 1 — Reduce Frequency

```
20Hz → 10Hz → 5Hz
```

### Step 2 — Reduce Output

* drop background vehicles
* keep anomalies + viewport only

### Step 3 — Cluster Mode

Instead of individual vehicles:

```
send clusters (H3 aggregated cells)
```

---

# 13. Attention Engine Usage

This system integrates:

```go
import "github.com/takara-ai/go-attention/attention"
```

## Example scoring:

```go
score, err := attention.DotProductAttention(
    clientQueryVector,
    vehicleKeyMatrix,
    vehicleValueMatrix,
)
```

---

# 14. Performance Expectations

| Metric            | Target                |
| ----------------- | --------------------- |
| Tick latency      | < 20ms                |
| Vehicle scoring   | ~100–300ns            |
| SSE batch build   | < 5ms                 |
| Max throughput    | 100k vehicles/sec     |
| Memory allocation | near-zero in hot path |

---

# 15. Running in Production

```bash
REGION_ID=nairobi-east \
TICK_RATE_HZ=20 \
ENABLE_BACKPRESSURE=true \
./projection-engine
```

---

# 16. Architecture Summary

```
                Sirtebasin
                    ↓
        Vehicle Stream (truth layer)
                    ↓
        Projection Engine (THIS SYSTEM)
                    ↓
        Go Attention Scoring Layer
                    ↓
        Policy + Backpressure Layer
                    ↓
        SSE / WebSocket Clients
```

---

# 17. Mental Model

This system is NOT:

* a database
* a stream processor
* a map renderer

It IS:

> a real-time **attention filter over geospatial reality**

---

# 18. Troubleshooting

### High latency

* reduce tick rate
* enable Redis caching
* check Sirtebasin query latency

### Too many vehicles sent

* lower MAX_VEHICLES_PER_CLIENT
* increase scoring threshold

### Missing updates

* check backpressure mode
* verify Sirtebasin stream health

---

# 19. Final Note

If Sirtebasin is **truth**, then this engine is **perception control**.

It does not store reality.

It decides what reality the user is allowed to see in real time.

```
```
