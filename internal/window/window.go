package window

import (
	"errors"
	"sort"

	"hypnotz/internal/types"
)

var (
	ErrLateEvent         = errors.New("late event rejected")
	ErrWatermarkViolation = errors.New("watermark violation")
)

// Window holds a time-bounded slice of VehicleState observations for one
// vehicle, produced by the sliding-window policy.
type Window struct {
	VehicleID   string
	StartTimeNS int64
	EndTimeNS   int64
	States      []types.VehicleState
	WatermarkNS int64
	IsFinalized bool
}

// WindowPolicy defines the temporal segmentation parameters.
type WindowPolicy struct {
	WindowSizeNS      int64
	SlideNS           int64
	AllowedLatenessNS int64
}

// StreamEvent wraps a VehicleState with an ingestion arrival timestamp.
type StreamEvent struct {
	State         types.VehicleState
	ArrivalTimeNS int64
}

// WindowState holds the mutable bookkeeping for the WindowingEngine.
type WindowState struct {
	Active          map[int64]*Window
	Finalized       []*Window
	MaxObservedTime int64
	Watermark       int64
}

// WindowingEngine implements a sliding-window policy with watermark-based
// late-event handling and deterministic replay.
type WindowingEngine struct {
	policy     WindowPolicy
	state      *WindowState
	deadLetter []StreamEvent
}

// DefaultWindowPolicy returns production-safe defaults.
func DefaultWindowPolicy() WindowPolicy {
	return WindowPolicy{
		WindowSizeNS:      60 * int64(1e9),
		SlideNS:           30 * int64(1e9),
		AllowedLatenessNS: 5 * int64(1e9),
	}
}

// NewWindowingEngine constructs an engine with the given policy.
func NewWindowingEngine(policy WindowPolicy) *WindowingEngine {
	return &WindowingEngine{
		policy: policy,
		state: &WindowState{
			Active:    make(map[int64]*Window),
			Finalized: make([]*Window, 0),
			Watermark: 0,
		},
		deadLetter: make([]StreamEvent, 0),
	}
}

// Ingest adds event to the appropriate active window. Returns ErrLateEvent
// when the event timestamp is below the allowed-lateness threshold and the
// event is routed to the dead-letter queue instead.
func (we *WindowingEngine) Ingest(event StreamEvent) error {
	if event.State.TimestampNS < we.state.Watermark-we.policy.AllowedLatenessNS {
		we.deadLetter = append(we.deadLetter, event)
		return ErrLateEvent
	}

	windowStart := AssignWindow(event.State, we.policy)
	win, exists := we.state.Active[windowStart]
	if !exists {
		win = &Window{
			VehicleID:   event.State.VehicleID,
			StartTimeNS: windowStart,
			EndTimeNS:   windowStart + we.policy.WindowSizeNS,
			States:      make([]types.VehicleState, 0),
			WatermarkNS: we.state.Watermark,
			IsFinalized: false,
		}
		we.state.Active[windowStart] = win
	}

	// Deduplicate within the window.
	for _, existing := range win.States {
		if existing.VehicleID == event.State.VehicleID &&
			existing.TimestampNS == event.State.TimestampNS {
			return nil
		}
	}

	win.States = append(win.States, event.State)
	sort.Slice(win.States, func(i, j int) bool {
		return win.States[i].TimestampNS < win.States[j].TimestampNS
	})

	if event.State.TimestampNS > we.state.MaxObservedTime {
		we.state.MaxObservedTime = event.State.TimestampNS
		we.AdvanceWatermark(we.state.MaxObservedTime)
	}

	return nil
}

// AdvanceWatermark moves the watermark to now − AllowedLatenessNS and
// finalises any active windows whose end time falls before the new watermark.
func (we *WindowingEngine) AdvanceWatermark(now int64) {
	newWatermark := now - we.policy.AllowedLatenessNS
	if newWatermark <= we.state.Watermark {
		return
	}
	we.state.Watermark = newWatermark
	for windowStart, win := range we.state.Active {
		if win.EndTimeNS < we.state.Watermark {
			win.IsFinalized = true
			we.state.Finalized = append(we.state.Finalized, win)
			delete(we.state.Active, windowStart)
		}
	}
}

// Emit returns all finalized windows since the last call.
func (we *WindowingEngine) Emit() ([]*Window, error) {
	result := make([]*Window, len(we.state.Finalized))
	copy(result, we.state.Finalized)
	return result, nil
}

// Stats returns the current count of active (open) and finalized windows.
func (we *WindowingEngine) Stats() (active, finalized int) {
	return len(we.state.Active), len(we.state.Finalized)
}

// CurrentWatermark returns the current watermark timestamp in nanoseconds.
func (we *WindowingEngine) CurrentWatermark() int64 { return we.state.Watermark }

// Finalized returns all windows that have been finalised.
func (we *WindowingEngine) Finalized() []*Window { return we.state.Finalized }

// GetDeadLetter returns events rejected due to excessive lateness.
func (we *WindowingEngine) GetDeadLetter() []StreamEvent { return we.deadLetter }

// Reset clears all window state and the dead-letter queue.
func (we *WindowingEngine) Reset() {
	we.state = &WindowState{
		Active:    make(map[int64]*Window),
		Finalized: make([]*Window, 0),
		Watermark: 0,
	}
	we.deadLetter = make([]StreamEvent, 0)
}

// AssignWindow returns the window start timestamp for state under policy.
func AssignWindow(v types.VehicleState, policy WindowPolicy) int64 {
	return (v.TimestampNS / policy.SlideNS) * policy.SlideNS
}

// AddState appends state to w, deduplicating by (VehicleID, TimestampNS).
func (w *Window) AddState(state types.VehicleState) {
	for _, existing := range w.States {
		if existing.VehicleID == state.VehicleID &&
			existing.TimestampNS == state.TimestampNS {
			return
		}
	}
	w.States = append(w.States, state)
	sort.Slice(w.States, func(i, j int) bool {
		return w.States[i].TimestampNS < w.States[j].TimestampNS
	})
}

// Finalize marks w as closed to further ingestion.
func (w *Window) Finalize() { w.IsFinalized = true }
