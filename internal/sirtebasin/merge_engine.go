package sirtebasin

import (
	"fmt"
	"time"

	"hypnotz/internal/types"
)

// MergeEngineImpl reconciles VehicleState observations from Redis (realtime)
// and ClickHouse (historical) into a single authoritative set.
type MergeEngineImpl struct {
	// stabilityThreshold is the minimum age a ClickHouse record must have
	// before it is preferred over a same-timestamp Redis record.
	stabilityThreshold time.Duration
}

// NewMergeEngine returns a MergeEngine with production defaults.
func NewMergeEngine() MergeEngine {
	return &MergeEngineImpl{
		stabilityThreshold: 5 * time.Minute,
	}
}

// Merge reconciles redis and clickhouse slices into a single deduplicated
// slice. When both sources contain a record for the same vehicle, Resolve
// selects the winner.
func (m *MergeEngineImpl) Merge(redis []types.VehicleState, clickhouse []types.VehicleState) []types.VehicleState {
	if len(redis) == 0 {
		return clickhouse
	}
	if len(clickhouse) == 0 {
		return redis
	}

	merged := make(map[string]types.VehicleState, len(redis))
	for _, s := range redis {
		merged[s.VehicleID] = s
	}
	for _, s := range clickhouse {
		if existing, ok := merged[s.VehicleID]; ok {
			merged[s.VehicleID] = Resolve(existing, s, m.stabilityThreshold)
		} else {
			merged[s.VehicleID] = s
		}
	}

	result := make([]types.VehicleState, 0, len(merged))
	for _, s := range merged {
		result = append(result, s)
	}
	return result
}

// Resolve selects the authoritative state between two observations for the
// same vehicle. Resolution order:
//  1. Newer timestamp wins outright.
//  2. At equal timestamps, a stable ClickHouse record beats Redis.
//  3. Otherwise, higher IngestSeq wins.
func Resolve(a, b types.VehicleState, stabilityThreshold time.Duration) types.VehicleState {
	if a.TimestampNS > b.TimestampNS {
		return a
	}
	if b.TimestampNS > a.TimestampNS {
		return b
	}

	// Equal timestamps: prefer a stable ClickHouse record over Redis.
	if a.DataSource == types.SourceClickHouse && b.DataSource == types.SourceRedis {
		if isClickHouseStable(a, stabilityThreshold) {
			return a
		}
		return b
	}
	if b.DataSource == types.SourceClickHouse && a.DataSource == types.SourceRedis {
		if isClickHouseStable(b, stabilityThreshold) {
			return b
		}
		return a
	}

	// Fall back to ingest sequence.
	if a.IngestSeq > b.IngestSeq {
		return a
	}
	return b
}

// isClickHouseStable reports whether the ClickHouse record is old enough to
// be considered stable (i.e. it has survived compaction and is trustworthy).
func isClickHouseStable(state types.VehicleState, threshold time.Duration) bool {
	return time.Since(state.Timestamp) > threshold
}

// ComputeConfidence derives a [0, 1] confidence score for state at the given
// time, blending recency decay with a per-source bonus.
func ComputeConfidence(state types.VehicleState, now time.Time) float64 {
	age := float64(now.Sub(state.Timestamp))
	base := 1.0 - (age / float64(types.MaxAgeNS))

	var sourceBonus float64
	switch state.DataSource {
	case types.SourceRedis:
		sourceBonus = 0.15
	case types.SourceClickHouse:
		sourceBonus = 0.10
	case types.SourceMerged:
		sourceBonus = 0.05
	}

	return clamp(base+sourceBonus, 0.0, 1.0)
}

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

// Deduplicate removes duplicate (vehicleID, timestampNS) entries, keeping the
// first occurrence of each pair.
func Deduplicate(states []types.VehicleState) []types.VehicleState {
	seen := make(map[string]struct{}, len(states))
	result := make([]types.VehicleState, 0, len(states))
	for _, s := range states {
		key := fmt.Sprintf("%s:%d", s.VehicleID, s.TimestampNS)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, s)
	}
	return result
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
