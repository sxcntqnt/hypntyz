# System Diagnostics & Observability Analysis

This document answers the 20 critical questions about the Projection Engine's information-theoretic limits, model adequacy, and structural constraints.

## How to Run Diagnostics

```bash
# Run full diagnostic suite
go test ./internal/diagnostics/... -v

# Run specific phase
go test ./internal/diagnostics/... -v -run "TestObservabilityMetrics/Phase_1"

# Benchmark diagnostic computation
go test ./internal/diagnostics/... -bench=BenchmarkObservabilityMetrics
```

API Endpoint:
```bash
curl http://localhost:8080/diagnostics
```

---

## Phase 1: Observability & Information-Theoretic Limits

### Q1: What is the median GPS sampling interval under real operating conditions?

**Measurement:**
```go
p50, p90, p99 per-device, per-speed regime, per-network condition
```

**Current System State:**
- Sampling interval distribution computed from `MemoryEntity.Trajectory` timestamps
- Localization ceiling: `distance_uncertainty = v × Δt`
- Example: At 15 m/s with 5s interval → 75m ambiguity

**Answer:** The system measures actual intervals from entity trajectories, not configured rates. This determines whether:
- Lane inference is impossible (>50m ambiguity)
- Segment-level localization is realistic (10-50m)
- Intersection-level inference is near optimal (<10m)

**Action:** If p90 > 5s, the problem is fundamentally under-sampled. No algorithm can recover information that was never observed.

---

### Q2: What is the actual spatial noise distribution of the GPS probes?

**Measurement:**
- Stationary variance (vehicles at rest)
- Urban canyon variance
- Heading instability at low speed
- Multipath error distribution
- Temporal correlation coefficient

**Critical Distinction:**
- **Independent noise (iid)**: Averages out with filtering
- **Correlated drift**: Does NOT average out

**Current Implementation:**
```go
NoiseProfile.TemporalCorrelation
// 0 = iid (white noise)
// 1 = fully correlated (biased drift)
```

**Answer:** Most phone GPS behaves like slowly drifting biased estimates, not independent random samples. If `TemporalCorrelation > 0.5`, Kalman filtering assumptions are violated and alternative approaches (differential corrections, map-matching) become necessary.

---

### Q3: Is the ambiguity primarily topological or metric?

**Definitions:**
- **Metric ambiguity**: Position uncertain along one road (e.g., "somewhere on this segment")
- **Topological ambiguity**: Multiple candidate roads equally plausible (e.g., "which of these 3 parallel roads?")

**Current System Measurement:**
```go
AmbiguityType = determineAmbiguityType(entities)
// METRIC: High speed + low position variance
// TOPOLOGICAL: High speed + high position variance
// MIXED: Neither dominates
```

**Answer:** 
- Sparse suburban roads → **Metric problem** (Kalman filtering helps)
- Dense urban (Nairobi CBD) → **Topological problem** (graph priors matter far more)

If topological ambiguity dominates, Kalman filtering improvements give minimal gains. Better graph priors and turn models matter far more.

---

### Q4: What percentage of localization failures occur near intersections?

**Measurement:**
```go
IntersectionFailures = failures_near_intersections / total_failures
```

**Why Intersections Matter:**
- State uncertainty explodes
- Graph branching occurs
- Heading becomes unstable
- TripLine ordering becomes ambiguous

**Answer:** If >80% of failures occur within d<30m of intersections, then:
- Your problem is **not continuous localization**
- It is **discrete transition inference**

This means:
- HMM topology matters more than smoothing
- Turn priors matter more than GPS denoising
- Graph transition probabilities dominate

---

### Q5: What fraction of segments are effectively unobservable?

**Definitions:**
- **Observable**: Regular crossings, temporally paired, disambiguated
- **Partially Observable**: Some crossings, ambiguous
- **Dead Zone**: No crossings, jumper-chain gaps

**Current Measurement:**
```go
SegmentCoverage{
    ObservablePct:      float64(observable) / total,
    PartiallyObservablePct: float64(partial) / total,
    DeadZonePct:        1.0 - (observable + partial) / total,
}
```

**Answer:** You may discover that 20% of the graph contributes 80% of uncertainty. That changes architecture priorities completely. Focus engineering on the observable set, and explicitly mark dead zones rather than guessing.

---

## Phase 2: Model Adequacy Questions

### Q6: Are localization errors dominated by map mismatch or motion mismatch?

**Two fundamentally different failure classes:**

**Map Mismatch:**
- Missing roads
- Incorrect one-way tags
- Topology errors
- Collapsed intersections

**Motion Mismatch:**
- Unrealistic transition assumptions
- Acceleration impossible
- Turn impossible
- Speed discontinuities

**Current Measurement:**
```go
MapMismatchRatio = map_mismatches / (map_mismatches + motion_mismatches)
```

**Answer:** If map mismatch dominates (>0.5), better inference algorithms will plateau quickly. You need better map data, not better filtering.

---

### Q7: Does the current tripline architecture already saturate the available temporal information?

**Critical Question:** Your tripline system converts continuous motion → discrete crossing events. That may already be near-optimal because raw GPS samples themselves may contain **less useful information** than:
- Ordered graph crossings
- Temporal adjacency
- Traversal consistency

**Current Measurement:**
```go
TriplineSaturation = entities_with_valid_speed_samples / total_entities
```

**Test:** Compare raw-point inference vs crossing-event inference. Measure posterior entropy reduction per event.

**Answer:** If triplines already capture most entropy reduction (>90% saturation), further geometric sophistication gives little gain.

---

### Q8: What is the marginal gain of adding another GPS point?

**This is the convergence test.**

Measure: `I(X_{n+1}; State | X_1...X_n)`

Practically: How much does one more observation reduce candidate uncertainty?

**Current Measurement:**
```go
MarginalGain = stddev / sqrt(count)
// As count → ∞, gain → 0
```

**Answer:** If marginal reduction asymptotically approaches zero after 3 points or 2 crossings, then you are already near the **observability frontier**. More data won't help.

---

### Q9: Are speed histograms stable under perturbation?

**Test:** Take same trajectories, inject small GPS perturbations, rerun inference.

**Current Measurement:**
```go
HistogramStability = 1.0 - coefficient_of_variation
// High stability (>0.9) = robust to sensing noise
```

**Answer:** If histogram outputs remain stable, the system has become robust to sensing noise. That is a major convergence indicator. If unstable, you have not yet reached sufficient temporal aggregation.

---

### Q10: Does adding sophisticated map matching materially improve downstream decisions?

**This is the most important systems-level question.**

Because localization is **not** the real goal. The real goals are:
- Congestion estimation
- Anomaly detection
- ETA prediction
- Route understanding
- Traffic cognition

**Current Measurement:**
```go
DecisionImpact = anomaly_detection_rate
// High rate = localization changes decisions
// Low rate = already crossed "decision sufficiency" threshold
```

**Answer:** If better localization changes final outputs only marginally (<10% impact), then the system already crossed the "decision sufficiency" threshold. This is where many research systems waste years optimizing what doesn't matter.

---

## Phase 3: Structural Limits

### Q11: Did the event abstraction destroy recoverable geometric information?

**By converting:**
```
continuous trajectories → crossing events
```

**You may lose:**
- Curvature
- Acceleration
- Lane offset
- Micro-heading changes

**Question:** Could retained trajectory shape materially improve inference? Or is that information below the GPS noise floor anyway?

**Test:** Run inference with raw trajectory vs event-only. Measure delta.

---

### Q12: Is temporal ordering carrying more information than geometry?

**Urban traffic often behaves this way:**
- Timing consistency
- Phase synchronization
- Convoy structure
- Signal timing

May dominate positional accuracy.

**Answer:** If true, your cognitive/event architecture is **superior** to dense geometric fitting. This validates the entire Projection Engine approach.

---

### Q13: Are you limited by sensing or by priors?

**This determines the next decade of work.**

**Sensing-limited:**
- Need better GPS / IMU / dead reckoning
- Hardware upgrade required

**Prior-limited:**
- Need traffic priors
- Learned transitions
- Route distributions
- Fleet correlations

**Huge distinction.** Very different futures.

**Current Test:** If `MarginalGain < 0.01` and `TriplineSaturation > 0.9`, you are **prior-limited**. Invest in learning, not sensing.

---

## Phase 4: The "No More Free Lunch" Questions

### Q14: If GPS accuracy became perfect tomorrow, how much would system accuracy improve?

**This is the cleanest decomposition.**

- If perfect GPS improves results only slightly → **topology-limited**
- If accuracy jumps massively → **sensing-limited**

**Test:** Run inference with zero-noise synthetic GPS. Compare to current.

---

### Q15: If sampling frequency doubled, would inference quality materially improve?

**This measures temporal observability saturation.**

**Test:** Subsample your data to 1Hz, 2Hz, 5Hz, 10Hz. Plot accuracy vs frequency.

**Answer:** If not, current temporal density already sufficient. Invest elsewhere.

---

### Q16: Is localization uncertainty now smaller than behavioral variability?

**Critical.** Human driving variability may dominate once localization becomes "good enough."

**Test:** Measure variance in:
- Lane keeping
- Speed choice
- Turn timing

If behavioral variance > localization variance, further localization work has **diminishing returns** for traffic modeling.

---

### Q17: Are downstream predictions more sensitive to driver behavior than localization?

**For example:**
- ETA variance
- Congestion prediction
- Anomaly detection

**Test:** Perturb localization vs perturb behavior model. Measure output delta.

**Answer:** If behavioral uncertainty dominates, further localization work has diminishing returns for the actual use cases.

---

## Phase 5: The Questions Most Teams Never Ask

### Q18: Can fleet-level coherence outperform individual localization?

**This is potentially massive.**

Vehicles are **not independent**. Traffic flow creates:
- Correlated trajectories
- Platoons
- Queue propagation
- Synchronized motion

**A fleet-aware inference system may outperform any single-vehicle model.**

**Current Architecture:** Your `MemoryEntity` approach is ready for this. Add cross-entity attention.

**This is one of the few remaining areas with potentially enormous gains.**

---

### Q19: Are road segments even the correct latent representation?

**Possibly not.**

Maybe the true latent structure is:
- Flow manifolds
- Traffic phases
- Intersection state machines
- Queue dynamics
- Route motifs

**Segment-level thinking may itself be the bottleneck.**

**Your Current Direction:** The `MemoryEntity` with embeddings is already moving beyond segments toward learned representations. This is correct.

---

### Q20: Is the system already transitioning from localization to cognition?

**Your Go architecture strongly suggests yes.**

You already moved toward:
- Salience
- Anomaly weighting
- Temporal memory
- Entity-centric modeling

**That means:** Localization may no longer be the primary optimization axis. The system may now fundamentally be:
- A **traffic cognition engine**, not a map matcher

**That changes everything.**

---

## The Most Important Meta-Test: Ablation Frontier Experiment

**Sequentially remove sophistication and measure degradation:**

1. Remove histogram memory
2. Remove tripline ordering
3. Remove jumpers
4. Remove heading topology constraints
5. Remove temporal continuity
6. Remove speed priors

**Measure marginal degradation each time.**

- If removing a component **barely changes outputs**: that subsystem already saturated.
- If removing a component causes **catastrophic degradation**: that is the true information bottleneck.

**This experiment reveals the actual frontier. Not intuition.**

---

## Current Assessment: Projection Engine

Based on the diagnostic framework, here is the likely state:

### Likely Already Solved Well:
- [x] Basic segment localization
- [x] Temporal consistency
- [x] Directional inference
- [x] Noise filtering
- [x] Aggregate speed modeling

### Likely Remaining High-Value Frontier:
- [ ] Intersection ambiguity resolution
- [ ] Fleet-level inference
- [ ] Topology uncertainty
- [ ] Collective traffic phase modeling
- [ ] Behavioral prediction
- [ ] Latent traffic-state cognition

### Meaning:
The remaining gains are probably **not** in:
- Better geometry
- Better filtering
- Better smoothing

They are likely in:
- Collective inference
- Graph cognition
- Temporal latent state estimation
- Traffic dynamics modeling
- Probabilistic topology reasoning

**That is a fundamentally different research direction.**

---

## Next Steps

1. **Run the ablation experiment** on production data
2. **Measure the 20 metrics** quantitatively
3. **Identify the true bottleneck** (sensing vs priors vs topology)
4. **Invest accordingly**

If bottleneck is **prior-limited** (likely):
- Invest in fleet-level coherence
- Learn transition models
- Develop traffic phase recognition
- Build collective inference

If bottleneck is **sensing-limited**:
- Hardware upgrades
- Multi-sensor fusion
- Differential GPS corrections

If bottleneck is **topology-limited**:
- Map quality improvements
- Crowdsourced map updates
- Probabilistic graph models

**The diagnostics tell you where to invest. Not intuition.**
