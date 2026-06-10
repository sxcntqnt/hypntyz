package window_test

import (
	"fmt"
	"testing"

	"hypnotz/internal/types"
	"hypnotz/internal/window"
)

// ─── Property helpers ──────────────────────────────────────────────────────────

func Prop_DeterministicWindows(policy window.WindowPolicy, events []window.StreamEvent) bool {
	e1 := window.NewWindowingEngine(policy)
	e2 := window.NewWindowingEngine(policy)

	for _, e := range events {
		e1.Ingest(e) //nolint:errcheck
		e2.Ingest(e) //nolint:errcheck
	}

	w1, _ := e1.Emit()
	w2, _ := e2.Emit()

	if len(w1) != len(w2) {
		return false
	}
	for i := range w1 {
		if w1[i].StartTimeNS != w2[i].StartTimeNS ||
			w1[i].EndTimeNS != w2[i].EndTimeNS {
			return false
		}
	}
	return true
}

func Prop_NoDuplicateEvents(policy window.WindowPolicy, events []window.StreamEvent) bool {
	eng := window.NewWindowingEngine(policy)
	for _, e := range events {
		eng.Ingest(e) //nolint:errcheck
	}

	windows, _ := eng.Emit()
	seen := make(map[string]struct{})
	for _, w := range windows {
		for _, s := range w.States {
			// Use a properly formatted string key — string(rune(int64)) is
			// not a digit string and produces collisions for large values.
			key := fmt.Sprintf("%s:%d", s.VehicleID, s.TimestampNS)
			if _, dup := seen[key]; dup {
				return false
			}
			seen[key] = struct{}{}
		}
	}
	return true
}

func Prop_WatermarkMonotonic(policy window.WindowPolicy, events []window.StreamEvent) bool {
	eng := window.NewWindowingEngine(policy)
	last := int64(0)
	for _, e := range events {
		eng.Ingest(e) //nolint:errcheck
		wm := eng.CurrentWatermark()
		if wm < last {
			return false
		}
		last = wm
	}
	return true
}

func Prop_FinalizedImmutability(policy window.WindowPolicy, events []window.StreamEvent) bool {
	eng := window.NewWindowingEngine(policy)
	for _, e := range events {
		eng.Ingest(e) //nolint:errcheck
	}
	snapshot, _ := eng.Emit()

	for _, e := range events {
		eng.Ingest(e) //nolint:errcheck
	}
	final, _ := eng.Emit()

	return len(snapshot) == len(final)
}

// ─── Unit tests ────────────────────────────────────────────────────────────────

func TestDeterministicWindowing(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 200}, ArrivalTimeNS: 200},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 300}, ArrivalTimeNS: 300},
	}
	if !Prop_DeterministicWindows(policy, events) {
		t.Error("windowing should be deterministic")
	}
}

func TestNoDuplicateEvents(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 150}, // duplicate
	}
	if !Prop_NoDuplicateEvents(policy, events) {
		t.Error("should not allow duplicate (vehicleID, timestampNS) pairs")
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
		t.Error("watermark must be monotonically non-decreasing")
	}
}

func TestFinalizedImmutability(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	events := []window.StreamEvent{
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 100}, ArrivalTimeNS: 100},
		{State: types.VehicleState{VehicleID: "v1", TimestampNS: 200}, ArrivalTimeNS: 200},
	}
	if !Prop_FinalizedImmutability(policy, events) {
		t.Error("re-ingesting the same events must not change finalized window count")
	}
}

func TestWindowAssignment(t *testing.T) {
	policy := window.WindowPolicy{
		WindowSizeNS:      100,
		SlideNS:           50,
		AllowedLatenessNS: 10,
	}
	cases := []struct {
		ts    int64
		start int64
	}{
		{0, 0},
		{49, 0},
		{50, 50},
		{99, 50},
		{100, 100},
	}
	for _, tc := range cases {
		got := window.AssignWindow(types.VehicleState{TimestampNS: tc.ts}, policy)
		if got != tc.start {
			t.Errorf("ts=%d: expected window start %d, got %d", tc.ts, tc.start, got)
		}
	}
}

func TestLateEventRejection(t *testing.T) {
	policy := window.WindowPolicy{
		WindowSizeNS:      100,
		SlideNS:           50,
		AllowedLatenessNS: 10,
	}
	eng := window.NewWindowingEngine(policy)

	eng.Ingest(window.StreamEvent{ //nolint:errcheck
		State:         types.VehicleState{VehicleID: "v1", TimestampNS: 100},
		ArrivalTimeNS: 100,
	})
	eng.AdvanceWatermark(200)

	err := eng.Ingest(window.StreamEvent{
		State:         types.VehicleState{VehicleID: "v1", TimestampNS: 50},
		ArrivalTimeNS: 300,
	})
	if err == nil {
		t.Error("late event should be rejected with ErrLateEvent")
	}
}

func TestSlidingWindowOverlap(t *testing.T) {
	policy := window.WindowPolicy{
		WindowSizeNS:      100,
		SlideNS:           50,
		AllowedLatenessNS: 10,
	}
	w1 := window.AssignWindow(types.VehicleState{TimestampNS: 75}, policy)
	w2 := window.AssignWindow(types.VehicleState{TimestampNS: 125}, policy)

	if w1 == w2 {
		t.Error("timestamps 75 and 125 should fall in different windows")
	}
	if diff := w2 - w1; diff != 50 {
		t.Errorf("expected window difference 50, got %d", diff)
	}
}

func TestStats(t *testing.T) {
	policy := window.DefaultWindowPolicy()
	eng := window.NewWindowingEngine(policy)

	active, finalized := eng.Stats()
	if active != 0 || finalized != 0 {
		t.Errorf("fresh engine should have 0/0 stats, got %d/%d", active, finalized)
	}

	eng.Ingest(window.StreamEvent{ //nolint:errcheck
		State:         types.VehicleState{VehicleID: "v1", TimestampNS: 1_000_000_000},
		ArrivalTimeNS: 1_000_000_000,
	})
	active, _ = eng.Stats()
	if active == 0 {
		t.Error("should have at least one active window after ingest")
	}
}

func TestDeadLetterQueue(t *testing.T) {
	policy := window.WindowPolicy{
		WindowSizeNS:      100,
		SlideNS:           50,
		AllowedLatenessNS: 10,
	}
	eng := window.NewWindowingEngine(policy)

	eng.AdvanceWatermark(1000)
	eng.Ingest(window.StreamEvent{ //nolint:errcheck
		State:         types.VehicleState{VehicleID: "v1", TimestampNS: 1},
		ArrivalTimeNS: 2000,
	})

	if len(eng.GetDeadLetter()) != 1 {
		t.Errorf("expected 1 dead-letter event, got %d", len(eng.GetDeadLetter()))
	}
}
