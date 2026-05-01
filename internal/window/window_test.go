package window_test

import (
	"reflect"
	"testing"

	"hypnotz/internal/types"
	"hypnotz/internal/window"
)

func Prop_DeterministicWindows(policy window.WindowPolicy, events []window.StreamEvent) bool {
	engine1 := window.NewWindowingEngine(policy)
	engine2 := window.NewWindowingEngine(policy)

	for _, e := range events {
		engine1.Ingest(e)
		engine2.Ingest(e)
	}

	windows1, _ := engine1.Emit()
	windows2, _ := engine2.Emit()

	if len(windows1) != len(windows2) {
		return false
	}

	for i := range windows1 {
		if windows1[i].StartTimeNS != windows2[i].StartTimeNS {
			return false
		}
		if windows1[i].EndTimeNS != windows2[i].EndTimeNS {
			return false
		}
	}

	return true
}

func Prop_NoDuplicateEvents(policy window.WindowPolicy, events []window.StreamEvent) bool {
	engine := window.NewWindowingEngine(policy)

	for _, e := range events {
		engine.Ingest(e)
	}

	windows, _ := engine.Emit()

	seen := make(map[string]bool)
	for _, w := range windows {
		for _, s := range w.States {
			key := s.VehicleID + string(rune(s.TimestampNS))
			if seen[key] {
				return false
			}
			seen[key] = true
		}
	}

	return true
}

func Prop_WatermarkMonotonic(policy window.WindowPolicy, events []window.StreamEvent) bool {
	engine := window.NewWindowingEngine(policy)
	last := int64(0)

	for _, e := range events {
		engine.Ingest(e)
		wm := engine.CurrentWatermark()
		if wm < last {
			return false
		}
		last = wm
	}

	return true
}

func Prop_FinalizedImmutability(policy window.WindowPolicy, events []window.StreamEvent) bool {
	engine := window.NewWindowingEngine(policy)

	for _, e := range events {
		engine.Ingest(e)
	}

	snapshot, _ := engine.Emit()

	for _, e := range events {
		engine.Ingest(e)
	}

	finalSnapshot, _ := engine.Emit()

	if len(snapshot) != len(finalSnapshot) {
		return false
	}

	return true
}

func TestDeterministicWindowing(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 200}, ArrivalTimeNS: 200},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 300}, ArrivalTimeNS: 300},
	}

	if !Prop_DeterministicWindows(policy, events) {
		t.Error("Windowing should be deterministic")
	}
}

func TestNoDuplicateEvents(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 150},
	}

	if !Prop_NoDuplicateEvents(policy, events) {
		t.Error("Should not allow duplicate events")
	}
}

func TestWatermarkMonotonicity(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 200}, ArrivalTimeNS: 200},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 300}, ArrivalTimeNS: 300},
	}

	if !Prop_WatermarkMonotonic(policy, events) {
		t.Error("Watermark should be monotonic")
	}
}

func TestFinalizedImmutability(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 200}, ArrivalTimeNS: 200},
	}

	if !Prop_FinalizedImmutability(policy, events) {
		t.Error("Finalized windows should be immutable")
	}
}

func TestWindowAssignment(t *testing.T) {
	policy := window.WindowPolicy{
		WindowSizeNS:      100,
		SlideNS:           50,
		AllowedLatenessNS: 10,
	}

	testCases := []struct {
		timestamp    int64
		expectedStart int64
	}{
		{0, 0},
		{49, 0},
		{50, 50},
		{99, 50},
		{100, 100},
	}

	for _, tc := range testCases {
		state := types.VehicleState{TimestampNS: tc.timestamp}
		start := window.AssignWindow(state, policy)
		if start != tc.expectedStart {
			t.Errorf("Expected window start %d for timestamp %d, got %d", tc.expectedStart, tc.timestamp, start)
		}
	}
}

func TestLateEventRejection(t *testing.T) {
	policy := window.WindowPolicy{
		WindowSizeNS:      100,
		SlideNS:           50,
		AllowedLatenessNS: 10,
	}

	engine := window.NewWindowingEngine(policy)

	engine.Ingest(window.StreamEvent{
		State: types.VehicleState{VehicleID: "v1", TimestampNS: 100},
		ArrivalTimeNS: 100,
	})

	engine.AdvanceWatermark(200)

	err := engine.Ingest(window.StreamEvent{
		State: types.VehicleState{VehicleID: "v1", TimestampNS: 50},
		ArrivalTimeNS: 300,
	})

	if err == nil {
		t.Error("Late event should be rejected")
	}
}

func TestSlidingWindowOverlap(t *testing.T) {
	policy := window.WindowPolicy{
		WindowSizeNS:      100,
		SlideNS:           50,
		AllowedLatenessNS: 10,
	}

	state1 := types.VehicleState{VehicleID: "v1", TimestampNS: 75}
	state2 := types.VehicleState{VehicleID: "v1", TimestampNS: 125}

	window1 := window.AssignWindow(state1, policy)
	window2 := window.AssignWindow(state2, policy)

	if window1 == window2 {
		t.Error("Should assign to different windows")
	}

	expectedDiff := int64(50)
	if window2 - window1 != expectedDiff {
		t.Errorf("Expected window difference %d, got %d", expectedDiff, window2-window1)
	}
}

func TestEmptyWindowPadding(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	engine := window.NewWindowingEngine(policy)

	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 10000000000}, ArrivalTimeNS: 10000000000},
	}

	for _, e := range events {
		engine.Ingest(e)
	}

	windows, _ := engine.Emit()

	for _, w := range windows {
		if len(w.States) == 0 {
			t.Error("Empty windows should be handled by padding strategy")
		}
	}

	_ = reflect.TypeOf(windows)
}
