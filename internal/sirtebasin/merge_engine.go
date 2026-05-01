package sirtebasin

import (
	"time"

	"hypnotz/internal/types"
)

type MergeEngineImpl struct {
	stabilityThreshold time.Duration
}

func NewMergeEngine() MergeEngine {
	return &MergeEngineImpl{
		stabilityThreshold: 5 * time.Minute,
	}
}

func (m *MergeEngineImpl) Merge(redis []types.VehicleState, clickhouse []types.VehicleState) []types.VehicleState {
	if len(redis) == 0 {
		return clickhouse
	}
	if len(clickhouse) == 0 {
		return redis
	}

	merged := make(map[string]types.VehicleState)

	for _, state := range redis {
		key := state.VehicleID
		merged[key] = state
	}

	for _, state := range clickhouse {
		key := state.VehicleID
		existing, exists := merged[key]

		if !exists {
			merged[key] = state
			continue
		}

		merged[key] = Resolve(existing, state, m.stabilityThreshold)
	}

	result := make([]types.VehicleState, 0, len(merged))
	for _, state := range merged {
		result = append(result, state)
	}

	return result
}

func Resolve(a, b types.VehicleState, stabilityThreshold time.Duration) types.VehicleState {
	if a.TimestampNS > b.TimestampNS {
		return a
	}
	if b.TimestampNS > a.TimestampNS {
		return b
	}

	if a.DataSource == types.ClickHouse && b.DataSource == types.Redis {
		if isClickHouseStable(a, stabilityThreshold) {
			return a
		}
		return b
	}

	if b.DataSource == types.ClickHouse && a.DataSource == types.Redis {
		if isClickHouseStable(b, stabilityThreshold) {
			return b
		}
		return a
	}

	if a.IngestSeq > b.IngestSeq {
		return a
	}
	return b
}

func isClickHouseStable(state types.VehicleState, threshold time.Duration) bool {
	now := time.Now()
	age := now.Sub(state.Timestamp)
	return age > threshold
}

func ComputeConfidence(state types.VehicleState, now time.Time) float64 {
	age := float64(now.Sub(state.Timestamp))
	maxAge := float64(300 * time.Second)

	base := 1.0 - (age / maxAge)

	var sourceBonus float64
	switch state.DataSource {
	case types.Redis:
		sourceBonus = 0.15
	case types.ClickHouse:
		sourceBonus = 0.10
	case types.Merged:
		sourceBonus = 0.05
	}

	score := base + sourceBonus

	if score < 0.0 {
		return 0.0
	}
	if score > 1.0 {
		return 1.0
	}

	return score
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

func Deduplicate(states []types.VehicleState) []types.VehicleState {
	seen := make(map[string]bool)
	result := make([]types.VehicleState, 0)

	for _, state := range states {
		key := state.VehicleID + ":" + string(rune(state.TimestampNS))

		if seen[key] {
			continue
		}

		seen[key] = true
		result = append(result, state)
	}

	return result
}
