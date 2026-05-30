package compiler

import (
	"errors"
	"fmt"

	"hypnotz/internal/types"
)

// ErrUnsortedInput is returned when the input window is not monotonically
// increasing in timestamp.
var ErrUnsortedInput = errors.New("input states must be sorted by timestamp")

// TemporalSequenceCompiler converts a slice of VehicleState observations into
// a fixed-dimension TensorSequence suitable for the cognitive pipeline.
type TemporalSequenceCompiler struct{}

// NewTemporalSequenceCompiler returns a ready-to-use compiler.
func NewTemporalSequenceCompiler() *TemporalSequenceCompiler {
	return &TemporalSequenceCompiler{}
}

// Compile builds a TensorSequence from a time-ordered window of states for a
// single vehicle. Returns ErrUnsortedInput if the window is not monotonically
// ordered by TimestampNS.
func (c *TemporalSequenceCompiler) Compile(vehicleID string, window []types.VehicleState) (types.TensorSequence, error) {
	n := len(window)
	if n == 0 {
		return types.TensorSequence{
			VehicleID:    vehicleID,
			IsTimeSorted: true,
		}, nil
	}

	for i := 1; i < n; i++ {
		if window[i].TimestampNS < window[i-1].TimestampNS {
			return types.TensorSequence{}, ErrUnsortedInput
		}
	}

	t0 := window[0].TimestampNS
	tMax := window[n-1].TimestampNS

	tokens := make([][]float64, n)
	timestamps := make([]int64, n)
	confidence := make([]float64, n)

	for i, v := range window {
		tokens[i] = []float64{
			v.Lat,
			v.Lon,
			v.Speed,
			v.Heading,
			v.Confidence,
			normalizeTime(v.TimestampNS, t0, tMax),
			sourceEncoding(v.DataSource),
		}
		timestamps[i] = v.TimestampNS
		confidence[i] = v.Confidence
	}

	return types.TensorSequence{
		VehicleID:    vehicleID,
		Tokens:       tokens,
		Timestamps:   timestamps,
		Confidence:   confidence,
		IsTimeSorted: true,
	}, nil
}

// BatchCompile compiles a map of vehicle windows concurrently. Returns on the
// first error encountered.
func (c *TemporalSequenceCompiler) BatchCompile(states map[string][]types.VehicleState) (map[string]types.TensorSequence, error) {
	results := make(map[string]types.TensorSequence, len(states))
	for vehicleID, window := range states {
		seq, err := c.Compile(vehicleID, window)
		if err != nil {
			return nil, err
		}
		results[vehicleID] = seq
	}
	return results, nil
}

// ─── Pure validation helpers (used by tests and upstream callers) ──────────────

// IsTimeSorted reports whether states are monotonically non-decreasing in
// TimestampNS.
func IsTimeSorted(states []types.VehicleState) bool {
	for i := 1; i < len(states); i++ {
		if states[i].TimestampNS < states[i-1].TimestampNS {
			return false
		}
	}
	return true
}

// HasNoDuplicates reports whether all (vehicleID, timestampNS) pairs are
// unique within states.
func HasNoDuplicates(states []types.VehicleState) bool {
	seen := make(map[string]struct{}, len(states))
	for _, s := range states {
		key := fmt.Sprintf("%s:%d", s.VehicleID, s.TimestampNS)
		if _, dup := seen[key]; dup {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

// ConfidenceBounded reports whether all confidence values are in [0, 1].
func ConfidenceBounded(states []types.VehicleState) bool {
	for _, s := range states {
		if s.Confidence < 0.0 || s.Confidence > 1.0 {
			return false
		}
	}
	return true
}

// FeatureDimConsistent reports whether every token in tokens has length dim.
func FeatureDimConsistent(tokens [][]float64, dim int) bool {
	for _, t := range tokens {
		if len(t) != dim {
			return false
		}
	}
	return true
}

// ComputeConfidence derives a confidence score for v relative to the current
// time (now in nanoseconds), blending age-based decay with a source bonus.
func ComputeConfidence(v types.VehicleState, now int64) float64 {
	age := float64(now - v.TimestampNS)
	base := 1.0 - (age / float64(types.MaxAgeNS))

	var sourceBonus float64
	switch v.DataSource {
	case types.SourceRedis:
		sourceBonus = 0.15
	case types.SourceClickHouse:
		sourceBonus = 0.10
	case types.SourceMerged:
		sourceBonus = 0.05
	}

	return clamp(base+sourceBonus, 0.0, 1.0)
}

// ─── Internal helpers ──────────────────────────────────────────────────────────

// normalizeTime maps t into [0, 1] relative to the window [t0, tMax].
// Returns 0 when the window has zero duration.
func normalizeTime(t, t0, tMax int64) float64 {
	if tMax == t0 {
		return 0.0
	}
	return float64(t-t0) / float64(tMax-t0)
}

// sourceEncoding maps a SourceType to a stable float64 token value.
//
//	SourceRedis      → 1.0
//	SourceClickHouse → 2.0
//	SourceMerged     → 1.5
//	unknown          → 0.0
func sourceEncoding(s types.SourceType) float64 {
	switch s {
	case types.SourceRedis:
		return 1.0
	case types.SourceClickHouse:
		return 2.0
	case types.SourceMerged:
		return 1.5
	default:
		return 0.0
	}
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
