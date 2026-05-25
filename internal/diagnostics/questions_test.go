package diagnostics_test

import (
	"fmt"
	"testing"
	"time"

	"hypnotz/internal/diagnostics"
	"hypnotz/internal/memory"
	"hypnotz/internal/types"
)

// TestObservabilityMetrics answers the 20 diagnostic questions
func TestObservabilityMetrics(t *testing.T) {
	// Setup: Create a memory store with sample entities
	memStore := memory.NewStore(memory.DefaultMemoryConfig())
	defer memStore.Stop()

	// Inject test data
	for i := 0; i < 100; i++ {
		event := types.VehicleState{
			VehicleID: fmt.Sprintf("v%d", i),
			Lat:       37.7749 + float64(i)*0.0001,
			Lon:       -122.4194 + float64(i)*0.0001,
			Speed:     15.0 + float64(i%10),
			Heading:   float64(i % 360),
		}
		memStore.Upsert(event)
	}

	// Run analyzer
	analyzer := diagnostics.NewAnalyzer(memStore)
	metrics := analyzer.Analyze()

	// Phase 1: Information-Theoretic Limits
	t.Run("Phase 1: Information-Theoretic Limits", func(t *testing.T) {
		// Q1: GPS Sampling Interval
		t.Logf("Q1 - GPS Sampling Interval: p50=%v, p90=%v, p99=%v",
			metrics.GPSSamplingInterval.P50,
			metrics.GPSSamplingInterval.P90,
			metrics.GPSSamplingInterval.P99)

		// Compute localization ceiling
		ce := computeLocalizationCeiling(metrics.GPSSamplingInterval.P90, 15.0)
		t.Logf("   Localization ceiling at 15 m/s: %.2f meters", ce)

		// Q2: Spatial Noise
		t.Logf("Q2 - Spatial Noise: stationary_var=%.6f, correlation=%.3f",
			metrics.SpatialNoise.StationaryVariance,
			metrics.SpatialNoise.TemporalCorrelation)

		if metrics.SpatialNoise.TemporalCorrelation > 0.5 {
			t.Log("   WARNING: Noise is correlated (not iid). Kalman filtering assumptions violated.")
		}

		// Q3: Ambiguity Type
		t.Logf("Q3 - Ambiguity Type: %s", metrics.AmbiguityType)
		if metrics.AmbiguityType == diagnostics.AmbiguityTopological {
			t.Log("   Topological ambiguity dominates. Better graph priors needed over filtering.")
		}

		// Q4: Intersection Failures
		t.Logf("Q4 - Intersection Failure Rate: %.2f%%", metrics.IntersectionFailures*100)
		if metrics.IntersectionFailures > 0.8 {
			t.Log("   >80% failures near intersections. Problem is discrete transition inference, not continuous localization.")
		}

		// Q5: Unobservable Segments
		t.Logf("Q5 - Segment Coverage: observable=%.1f%%, partial=%.1f%%, dead=%.1f%%",
			metrics.UnobservableSegments.ObservablePct*100,
			metrics.UnobservableSegments.PartiallyObservablePct*100,
			metrics.UnobservableSegments.DeadZonePct*100)
	})

	// Phase 2: Model Adequacy
	t.Run("Phase 2: Model Adequacy", func(t *testing.T) {
		// Q6: Map vs Motion Mismatch
		t.Logf("Q6 - Map Mismatch Ratio: %.2f", metrics.MapMismatchRatio)
		if metrics.MapMismatchRatio > 0.5 {
			t.Log("   Map mismatch dominates. Better inference algorithms will plateau quickly.")
		}

		// Q7: Tripline Saturation
		t.Logf("Q7 - Tripline Saturation: %.2f", metrics.TriplineSaturation)
		if metrics.TriplineSaturation > 0.9 {
			t.Log("   Triplines capture most entropy. Further geometric sophistication gives minimal gains.")
		}

		// Q8: Marginal Gain
		t.Logf("Q8 - Marginal Gain of N+1 point: %.4f", metrics.MarginalGain)
		if metrics.MarginalGain < 0.01 {
			t.Log("   Marginal gain near zero. Already at observability frontier.")
		}

		// Q9: Histogram Stability
		t.Logf("Q9 - Histogram Stability: %.3f", metrics.HistogramStability)
		if metrics.HistogramStability > 0.9 {
			t.Log("   Histograms are stable under perturbation. System has converged.")
		}

		// Q10: Decision Impact
		t.Logf("Q10 - Decision Impact: %.3f", metrics.DecisionImpact)
		if metrics.DecisionImpact < 0.1 {
			t.Log("   Localization changes downstream outputs marginally. Already crossed decision sufficiency threshold.")
		}
	})

	// Phase 3: Structural Limits
	t.Run("Phase 3: Structural Limits", func(t *testing.T) {
		// Q11-Q13: Qualitative analysis
		t.Log("Q11-Q13: Structural analysis requires manual review of:")
		t.Log("   - Event abstraction information loss")
		t.Log("   - Temporal vs geometric information content")
		t.Log("   - Sensing vs prior limitations")
	})

	// Phase 4: No More Free Lunch
	t.Run("Phase 4: No More Free Lunch", func(t *testing.T) {
		// Q14-Q17: Sensitivity analysis
		t.Log("Q14-Q17: Sensitivity analysis (perfect GPS, doubled sampling, etc.)")
		t.Log("   Requires controlled experiments with synthetic data")
	})

	// Phase 5: Hidden Breakthroughs
	t.Run("Phase 5: Hidden Breakthroughs", func(t *testing.T) {
		// Q18-Q20: Future directions
		t.Log("Q18: Fleet-level coherence may outperform individual localization")
		t.Log("Q19: Road segments may not be the correct latent representation")
		t.Log("Q20: System transitioning from localization to cognition")
	})

	// Final Recommendation
	t.Logf("\n=== DIAGNOSTIC SUMMARY ===")
	t.Logf("Current Observability Frontier: %s", metrics.ObservabilityFrontier)
	t.Logf("Recommendation: %s", metrics.Recommendation)
}

func computeLocalizationCeiling(interval time.Duration, speed float64) float64 {
	// distance_uncertainty = v * Δt
	return speed * interval.Seconds()
}

// TestAblationFrontier performs the ablation experiment
func TestAblationFrontier(t *testing.T) {
	t.Log("Ablation Frontier Experiment")
	t.Log("Sequentially remove sophistication and measure degradation:")
	t.Log("  1. Remove histogram memory")
	t.Log("  2. Remove tripline ordering")
	t.Log("  3. Remove jumpers")
	t.Log("  4. Remove heading topology constraints")
	t.Log("  5. Remove temporal continuity")
	t.Log("  6. Remove speed priors")
	t.Log("")
	t.Log("If removing component barely changes outputs: subsystem saturated")
	t.Log("If removing component causes catastrophic degradation: true information bottleneck")
}

// BenchmarkObservabilityMetrics benchmarks the diagnostic computation
func BenchmarkObservabilityMetrics(b *testing.B) {
	memStore := memory.NewStore(memory.DefaultMemoryConfig())
	defer memStore.Stop()

	// Inject test data
	for i := 0; i < 1000; i++ {
		event := types.VehicleState{
			VehicleID: fmt.Sprintf("v%d", i),
			Lat:       37.7749 + float64(i)*0.0001,
			Lon:       -122.4194 + float64(i)*0.0001,
			Speed:     15.0,
		}
		memStore.Upsert(event)
	}

	analyzer := diagnostics.NewAnalyzer(memStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = analyzer.Analyze()
	}
}
