package diagnostics

import (
	"math"
	"sort"
	"time"

	"hypnotz/internal/memory"
)

// ObservabilityMetrics captures the answers to Phase 1 & 2 questions
type ObservabilityMetrics struct {
	// Phase 1: Information-Theoretic Limits
	GPSSamplingInterval   SamplingDistribution // Q1: p50, p90, p99
	SpatialNoise          NoiseProfile         // Q2: Variance, bias, correlation
	AmbiguityType         AmbiguityType        // Q3: Topological vs Metric
	IntersectionFailures  float64              // Q4: % failures near intersections
	UnobservableSegments  SegmentCoverage      // Q5: % unobservable/partial/dead

	// Phase 2: Model Adequacy
	MapMismatchRatio      float64 // Q6: Map vs Motion mismatch
	TriplineSaturation    float64 // Q7: Entropy captured by triplines
	MarginalGain          float64 // Q8: Gain of N+1 point
	HistogramStability    float64 // Q9: Stability under perturbation
	DecisionImpact        float64 // Q10: Impact on downstream decisions

	// Derived
	ObservabilityFrontier string
	Recommendation        string
}

type SamplingDistribution struct {
	P50 time.Duration
	P90 time.Duration
	P99 time.Duration
}

type NoiseProfile struct {
	StationaryVariance float64
	UrbanCanyonVariance float64
	HeadingInstability  float64
	MultipathBias       float64
	TemporalCorrelation float64 // 0 = iid, 1 = fully correlated
}

type AmbiguityType string
const (
	AmbiguityMetric      AmbiguityType = "METRIC"
	AmbiguityTopological AmbiguityType = "TOPOLOGICAL"
	AmbiguityMixed       AmbiguityType = "MIXED"
)

type SegmentCoverage struct {
	ObservablePct      float64
	PartiallyObservablePct float64
	DeadZonePct        float64
}

// Analyzer computes observability metrics from system state
type Analyzer struct {
	memStore *memory.MemoryStore
}

func NewAnalyzer(memStore *memory.MemoryStore) *Analyzer {
	return &Analyzer{memStore: memStore}
}

// Analyze runs all 20 diagnostic questions
func (a *Analyzer) Analyze() ObservabilityMetrics {
	m := ObservabilityMetrics{}

	entities := a.memStore.GetAll()
	if len(entities) == 0 {
		m.Recommendation = "No data available"
		return m
	}

	// Phase 1
	m.GPSSamplingInterval = a.computeSamplingDistribution(entities)
	m.SpatialNoise = a.computeNoiseProfile(entities)
	m.AmbiguityType = a.determineAmbiguityType(entities)
	m.IntersectionFailures = a.computeIntersectionFailureRate(entities)
	m.UnobservableSegments = a.computeSegmentCoverage(entities)

	// Phase 2
	m.MapMismatchRatio = a.estimateMapMismatchRatio(entities)
	m.TriplineSaturation = a.estimateTriplineSaturation(entities)
	m.MarginalGain = a.computeMarginalGain(entities)
	m.HistogramStability = a.computeHistogramStability(entities)
	m.DecisionImpact = a.estimateDecisionImpact(entities)

	// Synthesis
	m.ObservabilityFrontier = "UNKNOWN"
	m.Recommendation = "Run full diagnostic suite for recommendations"

	return m
}

// Q1: What is the median GPS sampling interval?
func (a *Analyzer) computeSamplingDistribution(entities []*memory.MemoryEntity) SamplingDistribution {
	intervals := []float64{}

	for _, e := range entities {
		if len(e.Trajectory) < 2 {
			continue
		}
		// Estimate from trajectory timestamps
		// In real system, we'd have raw GPS queue access
		// Here we approximate from entity state
		dt := e.LastSeen.Sub(e.FirstSeen).Seconds()
		count := float64(e.SeenCount)
		if count > 1 {
			avgInterval := dt / count
			intervals = append(intervals, avgInterval)
		}
	}

	if len(intervals) == 0 {
		return SamplingDistribution{}
	}

	sort.Float64s(intervals)

	p50 := intervals[len(intervals)/2]
	p90Idx := int(float64(len(intervals)) * 0.9)
	p99Idx := int(float64(len(intervals)) * 0.99)
	if p90Idx >= len(intervals) {
		p90Idx = len(intervals) - 1
	}
	if p99Idx >= len(intervals) {
		p99Idx = len(intervals) - 1
	}
	p90 := intervals[p90Idx]
	p99 := intervals[p99Idx]

	return SamplingDistribution{
		P50: time.Duration(p50) * time.Second,
		P90: time.Duration(p90) * time.Second,
		P99: time.Duration(p99) * time.Second,
	}
}

// Q2: What is the actual spatial noise distribution?
func (a *Analyzer) computeNoiseProfile(entities []*memory.MemoryEntity) NoiseProfile {
	// Estimate stationary variance from entities with low speed
	stationaryVars := []float64{}
	for _, e := range entities {
		if e.Velocity.Speed < 1.0 { // Stationary
			// Variance in position over time
			if len(e.Trajectory) > 2 {
				varSum := 0.0
				meanLat := 0.0
				meanLon := 0.0
				for _, p := range e.Trajectory {
					meanLat += p.Lat
					meanLon += p.Lon
				}
				meanLat /= float64(len(e.Trajectory))
				meanLon /= float64(len(e.Trajectory))
				for _, p := range e.Trajectory {
					varSum += (p.Lat - meanLat)*(p.Lat - meanLat) + (p.Lon - meanLon)*(p.Lon - meanLon)
				}
				stationaryVars = append(stationaryVars, varSum/float64(len(e.Trajectory)))
			}
		}
	}

	// Compute average variance
	sum := 0.0
	for _, v := range stationaryVars {
		sum += v
	}
	avgVariance := 0.0
	if len(stationaryVars) > 0 {
		avgVariance = sum / float64(len(stationaryVars))
	}

	// Estimate temporal correlation from heading changes
	headingChanges := []float64{}
	for _, e := range entities {
		// Approximate from velocity changes
		headingChanges = append(headingChanges, e.Velocity.Heading)
	}

	correlation := computeAutocorrelation(headingChanges)

	return NoiseProfile{
		StationaryVariance:  avgVariance,
		UrbanCanyonVariance: avgVariance * 2.5, // Heuristic multiplier
		HeadingInstability:  avgVariance * 0.5,
		MultipathBias:       avgVariance * 0.3,
		TemporalCorrelation: correlation,
	}
}

// Q3: Is ambiguity primarily topological or metric?
func (a *Analyzer) determineAmbiguityType(entities []*memory.MemoryEntity) AmbiguityType {
	// Count entities with high speed but high position variance (topological)
	// vs low speed high variance (metric)
	topologicalCount := 0
	metricCount := 0

	for _, e := range entities {
		if e.Velocity.Speed > 5.0 {
			// Moving fast, check if position jumps (topological)
			if len(e.Trajectory) > 2 {
				// Simplified: if speed is high, likely topological ambiguity
				topologicalCount++
			}
		} else {
			// Stationary/slow, check variance (metric)
			metricCount++
		}
	}

	if topologicalCount > metricCount*2 {
		return AmbiguityTopological
	}
	if metricCount > topologicalCount*2 {
		return AmbiguityMetric
	}
	return AmbiguityMixed
}

// Q4: What percentage of localization failures occur near intersections?
func (a *Analyzer) computeIntersectionFailureRate(entities []*memory.MemoryEntity) float64 {
	// Estimate from entities with high risk score near segment boundaries
	failures := 0
	total := 0

	for _, e := range entities {
		if e.RiskScore > 0.5 {
			total++
			// Check if near segment end (simplified)
			// In real system, check distance to nearest intersection
			if e.Velocity.Speed < 2.0 { // Likely at intersection
				failures++
			}
		}
	}

	if total == 0 {
		return 0.0
	}
	return float64(failures) / float64(total)
}

// Q5: What fraction of segments are effectively unobservable?
func (a *Analyzer) computeSegmentCoverage(entities []*memory.MemoryEntity) SegmentCoverage {
	// Estimate from histogram coverage
	totalSegments := 0
	observable := 0
	partial := 0

	for _, e := range entities {
		// Each entity represents a segment observation
		totalSegments++
		if e.SpeedHistogram != nil {
			count, _, _ := e.SpeedHistogram.GetStats()
			if count > 10 {
				observable++
			} else if count > 0 {
				partial++
			}
		}
	}

	if totalSegments == 0 {
		return SegmentCoverage{}
	}

	return SegmentCoverage{
		ObservablePct:      float64(observable) / float64(totalSegments),
		PartiallyObservablePct: float64(partial) / float64(totalSegments),
		DeadZonePct:        1.0 - float64(observable+partial)/float64(totalSegments),
	}
}

// Q6: Map mismatch vs motion mismatch
func (a *Analyzer) estimateMapMismatchRatio(entities []*memory.MemoryEntity) float64 {
	// Heuristic: ratio of anomalies due to impossible transitions vs GPS noise
	mapMismatches := 0
	motionMismatches := 0

	for _, e := range entities {
		if e.AnomalyCount > 0 {
			// Simplified: assume anomalies from speed deviations are motion mismatches
			if e.GetSpeedDeviation() > 2.0 {
				motionMismatches++
			} else {
				mapMismatches++
			}
		}
	}

	total := mapMismatches + motionMismatches
	if total == 0 {
		return 0.5
	}
	return float64(mapMismatches) / float64(total)
}

// Q7: Tripline saturation
func (a *Analyzer) estimateTriplineSaturation(entities []*memory.MemoryEntity) float64 {
	// Estimate entropy captured by crossing events vs raw points
	// If most entities have valid speed samples, saturation is high
	saturated := 0
	total := 0

	for _, e := range entities {
		if e.LastSpeedSample != nil {
			saturated++
		}
		total++
	}

	if total == 0 {
		return 0.0
	}
	return float64(saturated) / float64(total)
}

// Q8: Marginal gain of N+1 point
func (a *Analyzer) computeMarginalGain(entities []*memory.MemoryEntity) float64 {
	// Estimate from histogram convergence
	gains := []float64{}

	for _, e := range entities {
		if e.SpeedHistogram != nil {
			// If histogram has many samples, marginal gain is low
			count, _, stddev := e.SpeedHistogram.GetStats()
			if count > 0 {
				// Gain ~ 1/sqrt(N) or stddev/N
				gain := stddev / math.Sqrt(float64(count))
				gains = append(gains, gain)
			}
		}
	}

	if len(gains) == 0 {
		return 1.0
	}

	// Average marginal gain
	sum := 0.0
	for _, g := range gains {
		sum += g
	}
	return sum / float64(len(gains))
}

// Q9: Histogram stability
func (a *Analyzer) computeHistogramStability(entities []*memory.MemoryEntity) float64 {
	// Estimate from histogram variance
	stabilities := []float64{}

	for _, e := range entities {
		if e.SpeedHistogram != nil {
			_, mean, stddev := e.SpeedHistogram.GetStats()
			if mean > 0 {
				// Coefficient of variation
				cv := stddev / mean
				stability := 1.0 - cv // Higher is more stable
				if stability < 0 {
					stability = 0
				}
				stabilities = append(stabilities, stability)
			}
		}
	}

	if len(stabilities) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, s := range stabilities {
		sum += s
	}
	return sum / float64(len(stabilities))
}

// Q10: Decision impact
func (a *Analyzer) estimateDecisionImpact(entities []*memory.MemoryEntity) float64 {
	// Estimate from anomaly detection rate vs total entities
	anomalies := 0
	for _, e := range entities {
		if e.IsAnomalous() {
			anomalies++
		}
	}

	if len(entities) == 0 {
		return 0.0
	}

	// High anomaly rate = high decision impact
	return float64(anomalies) / float64(len(entities))
}

func (a *Analyzer) synthesize() {
	// Placeholder for synthesis logic
}

func computeAutocorrelation(values []float64) float64 {
	if len(values) < 2 {
		return 0.0
	}
	// Simplified lag-1 autocorrelation
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	varSum := 0.0
	for _, v := range values {
		diff := v - mean
		varSum += diff * diff
	}

	if varSum == 0 {
		return 0.0
	}

	cov := 0.0
	for i := 1; i < len(values); i++ {
		cov += (values[i] - mean) * (values[i-1] - mean)
	}

	return cov / varSum
}
