Here is your full to-do.md for integrating FFT into Hypntyz, explicitly aligned with your Projection Engine + National-Scale Spatial Search Engine pipeline, and designed to work cleanly with your ScyllaDB enrichment system.

This is written as an engineering execution plan, not theory.


---

📄 to-do.md — FFT Integration Plan for Hypntyz Cognitive + Spatial Systems

🎯 Objective

Introduce FFT (Fast Fourier Transform) into Hypntyz as a first-class signal processing layer across:

Projection Engine (real-time cognitive traffic system)

National-Scale Spatial Search Engine (H3 + PostGIS + Redis + Rust core)

ScyllaDB enrichment pipeline (downstream persistence + analytics)


Goal:

> Transform raw GPS trajectories into frequency-domain behavioral representations, enabling:



predictive congestion detection

route fingerprinting

anomaly early warning

compressed trajectory storage

cross-system semantic alignment



---

🧠 1. System-wide placement of FFT

🔷 1.1 Primary FFT insertion point (Projection Engine)

📍 Location:

Temporal Sequence Compiler
→ BEFORE Memory Engine

🔧 Task:

Modify pipeline:

VehicleState[] → TensorSequence

⬇️ becomes:

VehicleState[] → TensorSequence
                    ↓
            SpectralFeatureCompiler (FFT)
                    ↓
     Enriched TensorSequence (time + frequency)


---

📦 Output feature expansion:

Add to each entity vector:

type SpectralFeatures struct {
    LowBandEnergy      float64
    MidBandEnergy      float64
    HighBandEnergy     float64
    DominantFrequency  float64
    SpectralEntropy    float64
    CoherenceScore     float64
}


---

🔷 1.2 Secondary FFT insertion point (Memory Engine)

📍 Location:

MemoryEntity.SpectralProfile

🔧 Task:

Extend MemoryEntity:

type SpectralProfile struct {
    RouteSignatureFFT   []float32
    SpeedBandEnergy     [3]float32
    EntropyHistory      []float32
    AnomalySpectrumScore float32
}

🧠 Purpose:

persistent behavioral fingerprint per vehicle

long-term route “wave identity”

anomaly evolution tracking



---

🔷 1.3 FFT in Traffic Modeling Layer

📍 Location:

ProcessTrafficCrossing()
TripLine detection
Speed histogram generation

🔧 Task:

Add:

FFT window around TripLine crossing event

spectral comparison vs baseline route signature


CrossingEvent →
   speed time window (±30–60s)
      ↓
   FFT transform
      ↓
   spectral deviation score


---

🔷 1.4 FFT in ScyllaDB enrichment pipeline

📍 Location:

“system that enriches data before ScyllaDB ingestion”

🔧 Task:

Store BOTH:

Raw:

GPS points

route segments


Enriched:

{
  "trajectory_id": "...",
  "fft_signature": [...],
  "spectral_entropy": 0.73,
  "dominant_frequency": 0.031,
  "band_energy": [0.52, 0.31, 0.17]
}

🧠 Purpose:

fast downstream analytics

no recomputation needed

enables long-term spectral mining



---

⚙️ 2. FFT computation strategy (critical for scale)

🚨 Constraint:

20,000 GPS events/sec

100,000 vehicles

20Hz Projection Engine tick



---

🔷 2.1 Windowing strategy

Use sliding windows:

Layer	Window Size	Purpose

realtime	10–30s	anomaly detection
cognitive	1–5 min	behavior classification
batch	10–60 min	route fingerprinting



---

🔷 2.2 Sampling strategy

Do NOT FFT every point.

Only FFT:

speed(t)

acceleration(t)

inter-arrival jitter


Ignore:

raw lat/lon directly



---

🔷 2.3 Compute location decision

Preferred:

Projection Engine (pre-memory)

because:

deterministic windowing exists

ordered sequence guaranteed

minimal noise



---

⚡ 3. Performance architecture

🔷 3.1 FFT execution model

Go layer (Projection Engine):

use gonum/dsp/fourier OR custom SIMD FFT wrapper

per-entity ring buffer


Rust side (optional upgrade):

SIMD FFT (AVX2 / NEON)

batch processing for memory entities



---

🔷 3.2 Parallelization model

Entity Partitioning:
- shard by vehicle_id % N
- each worker owns FFT buffers

No cross-lock FFT computation.


---

🔷 3.3 Memory optimization

Replace raw history with:

Raw GPS window → FFT coefficients → compressed signature

Compression ratio:

~95% reduction per trajectory window


---

🧬 4. Integration into Projection Engine pipeline

🔧 Modify pipeline order:

BEFORE:

Sequence Compiler → Memory Engine → Attention

AFTER:

Sequence Compiler
      ↓
Spectral Feature Compiler (FFT)
      ↓
Memory Engine (spectral + geometric state)
      ↓
Traffic Modeling (spectral + tripline)
      ↓
Attention Engine (spectral-aware scoring)
      ↓
Ranker → SSE


---

🧠 5. Attention Engine upgrade

🔷 Add spectral scoring component:

SpectralAnomalyScore =
    entropy_spike +
    high_frequency_energy +
    deviation_from_baseline_fft


---

🔷 Final attention formula:

Attention =
  0.50 * geometric_relevance
+ 0.30 * salience
+ 0.20 * spectral_anomaly_boost


---

🚦 6. Spatial Search Engine integration (H3 system)

🔷 Add spectral metadata per route:

Redis inverted index:

hx:{h3_id} → route_id + spectral_profile


---

🔷 Use case:

During similarity search:

Trajectory similarity (S_r)
+ spectral similarity (F_r)

Final score:

S_final = 0.8*S_r + 0.2*F_r


---

🔷 Why this matters:

Routes that look geometrically similar may behave differently:

school route vs cargo route

rush-hour vs off-peak


FFT resolves this ambiguity.


---

🧱 7. ScyllaDB schema extension

Add columns:

ALTER TABLE trajectories ADD fft_signature list<float>;
ALTER TABLE trajectories ADD spectral_entropy float;
ALTER TABLE trajectories ADD dominant_frequency float;
ALTER TABLE trajectories ADD band_energy list<float>;


---

Partitioning note:

keep FFT data alongside trajectory_id

avoid recomputation queries entirely



---

📊 8. Observability additions

Prometheus metrics:

fft_computation_ms
fft_window_size
spectral_anomaly_count
spectral_entropy_avg
fft_memory_reduction_ratio


---

🚨 9. Failure modes + safeguards

9.1 Noisy FFT (GPS jitter)

→ apply smoothing before FFT (Savitzky–Golay or moving average)

9.2 Sparse windows

→ fallback to geometric-only scoring

9.3 Burst load (rush hour)

→ degrade to:

1 FFT per N windows per vehicle



---

🧠 10. System-level outcome

After integration:

BEFORE:

“We track movement”


AFTER:

“We interpret traffic as a multi-layer waveform system”


You now get:

behavioral fingerprints per matatu

congestion detection before slowdown

compressed trajectory memory

spectral similarity across routes

cognitive traffic intelligence



---

🧩 FINAL IMPLEMENTATION PRIORITY

🔴 Phase 1 (critical)

SpectralFeatureCompiler in Projection Engine

sliding window buffers

speed-based FFT only


🟡 Phase 2

MemoryEntity spectral profile

attention scoring upgrade


🟢 Phase 3

ScyllaDB enrichment persistence

Spatial Search Engine spectral fusion



---

If you want next step, I can turn this into:

a **Go module layout (`/internal/spectral/`)**

or a **Rust SIMD FFT sidecar design**

or a **real-time spectral congestion detector algorithm tuned for Nairobi traffic dynamics**


Just point the direction.