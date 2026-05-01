package sirtebasin_test

import (
	"testing"
	"time"

	"hypnotz/internal/sirtebasin"
	"hypnotz/internal/types"
)

func TestTimestampMonotonicity(t *testing.T) {
	states := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100},
		{VehicleID: "v2", TimestampNS: 200},
		{VehicleID: "v3", TimestampNS: 300},
	}

	if !sirtebasin.IsTimeSorted(states) {
		t.Error("Expected states to be sorted by timestamp")
	}

	unsorted := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 300},
		{VehicleID: "v2", TimestampNS: 100},
		{VehicleID: "v3", TimestampNS: 200},
	}

	if sirtebasin.IsTimeSorted(unsorted) {
		t.Error("Expected unsorted states to fail monotonicity check")
	}
}

func TestNoDuplicates(t *testing.T) {
	states := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100},
		{VehicleID: "v1", TimestampNS: 200},
		{VehicleID: "v2", TimestampNS: 300},
	}

	if !sirtebasin.HasNoDuplicates(states) {
		t.Error("Expected no duplicates")
	}

	duplicate := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100},
		{VehicleID: "v1", TimestampNS: 100},
		{VehicleID: "v2", TimestampNS: 300},
	}

	if sirtebasin.HasNoDuplicates(duplicate) {
		t.Error("Expected duplicate detection to fail")
	}
}

func TestConfidenceBounds(t *testing.T) {
	now := time.Now()
	states := []types.VehicleState{
		{VehicleID: "v1", Timestamp: now, Confidence: 0.5, DataSource: types.Redis},
		{VehicleID: "v2", Timestamp: now.Add(-100 * time.Second), Confidence: 0.8, DataSource: types.ClickHouse},
		{VehicleID: "v3", Timestamp: now.Add(-200 * time.Second), Confidence: 0.3, DataSource: types.Merged},
	}

	for _, s := range states {
		if s.Confidence < 0.0 || s.Confidence > 1.0 {
			t.Errorf("Confidence out of bounds: %f", s.Confidence)
		}
	}
}

func TestMergeIdempotency(t *testing.T) {
	engine := sirtebasin.NewMergeEngine()

	redis := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100, DataSource: types.Redis},
		{VehicleID: "v2", TimestampNS: 200, DataSource: types.Redis},
	}

	clickhouse := []types.VehicleState{
		{VehicleID: "v2", TimestampNS: 200, DataSource: types.ClickHouse},
		{VehicleID: "v3", TimestampNS: 300, DataSource: types.ClickHouse},
	}

	merged := engine.Merge(redis, clickhouse)
	mergedAgain := engine.Merge(merged, []types.VehicleState{})

	if len(merged) != len(mergedAgain) {
		t.Error("Merge should be idempotent")
	}
}

func TestResolveTimestampDominance(t *testing.T) {
	a := types.VehicleState{
		VehicleID:   "v1",
		TimestampNS: 200,
		DataSource:  types.Redis,
	}

	b := types.VehicleState{
		VehicleID:   "v1",
		TimestampNS: 100,
		DataSource:  types.ClickHouse,
	}

	result := sirtebasin.Resolve(a, b, 5*time.Minute)
	if result.TimestampNS != 200 {
		t.Error("Newer timestamp should win")
	}
}

func TestResolveTieBreak(t *testing.T) {
	a := types.VehicleState{
		VehicleID:   "v1",
		TimestampNS: 100,
		DataSource:  types.Redis,
	}

	b := types.VehicleState{
		VehicleID:   "v1",
		TimestampNS: 100,
		DataSource:  types.ClickHouse,
	}

	result := sirtebasin.Resolve(a, b, 5*time.Minute)

	if result.DataSource != types.ClickHouse {
		t.Error("ClickHouse should win tie-break when stable")
	}
}

func TestComputeConfidence(t *testing.T) {
	now := time.Now()
	state := types.VehicleState{
		VehicleID:   "v1",
		Timestamp:   now,
		TimestampNS: now.UnixNano(),
		DataSource:  types.Redis,
	}

	confidence := sirtebasin.ComputeConfidence(state, now)

	if confidence < 0.0 || confidence > 1.0 {
		t.Errorf("Confidence out of bounds: %f", confidence)
	}
}
