package compiler

import (
	"errors"
	"math"
	"hypnotz/internal/types"
)

var (
	ErrUnsortedInput = errors.New("input states must be sorted by timestamp")
)

type TemporalSequenceCompiler struct{}

func NewTemporalSequenceCompiler() *TemporalSequenceCompiler {
	return &TemporalSequenceCompiler{}
}

func (c *TemporalSequenceCompiler) Compile(vehicleID string, window []types.VehicleState) (types.TensorSequence, error) {
	n := len(window)
	
	if n == 0 {
		return types.TensorSequence{
			VehicleID:  vehicleID,
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
		dt := normalizeTime(v.TimestampNS, t0, tMax)

		tokens[i] = []float64{
			v.Lat,
			v.Lon,
			v.Speed,
			v.Heading,
			v.Confidence,
			dt,
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

func normalizeTime(t, t0, tMax int64) float64 {
	if tMax == t0 {
		return 0.0
	}
	return float64(t-t0) / float64(tMax-t0)
}

func sourceEncoding(s types.SourceType) float64 {
	switch s {
	case types.Redis:
		return 1.0
	case types.ClickHouse:
		return 2.0
	case types.Merged:
		return 1.5
	default:
		return 0.0
	}
}

func (c *TemporalSequenceCompiler) BatchCompile(states map[string][]types.VehicleState) (map[string]types.TensorSequence, error) {
	results := make(map[string]types.TensorSequence)

	for vehicleID, window := range states {
		seq, err := c.Compile(vehicleID, window)
		if err != nil {
			return nil, err
		}
		results[vehicleID] = seq
	}

	return results, nil
}

func IsTimeSorted(states []types.VehicleState) bool {
	for i := 1; i < len(states); i++ {
		if states[i].TimestampNS < states[i-1].TimestampNS {
			return false
		}
	}
	return true
}

func HasNoDuplicates(states []types.VehicleState) bool {
	seen := make(map[string]bool)

	for _, s := range states {
		key := s.VehicleID + ":" + string(rune(s.TimestampNS))
		if seen[key] {
			return false
		}
		seen[key] = true
	}

	return true
}

func ConfidenceBounded(states []types.VehicleState) bool {
	for _, s := range states {
		if s.Confidence < 0.0 || s.Confidence > 1.0 {
			return false
		}
	}
	return true
}

func FeatureDimConsistent(tokens [][]float64, dim int) bool {
	for _, t := range tokens {
		if len(t) != dim {
			return false
		}
	}
	return true
}

func normalizeToRange(value, min, max float64) float64 {
	if max == min {
		return 0.0
	}
	return (value - min) / (max - min)
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

func computeConfidence(v types.VehicleState, now int64) float64 {
	age := float64(now - v.TimestampNS)
	base := 1.0 - (age / float64(types.MaxAgeNS))
	
	var sourceBonus float64
	switch v.DataSource {
	case types.Redis:
		sourceBonus = 0.15
	case types.ClickHouse:
		sourceBonus = 0.10
	case types.Merged:
		sourceBonus = 0.05
	}

	score := base + sourceBonus
	return clamp(score, 0.0, 1.0)
}

func sourceToFloat(s types.SourceType) float64 {
	switch s {
	case types.Redis:
		return 1.0
	case types.ClickHouse:
		return 2.0
	case types.Merged:
		return 1.5
	default:
		return 0.0
	}
}

func timeDelta(t int64, t0 int64) float64 {
	return float64(t - t0)
}

func _unused() {
	_ = math.Abs
}
