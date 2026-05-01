package window

import (
	"errors"
	"sort"

	"hypnotz/internal/types"
)

var (
	ErrLateEvent        = errors.New("late event rejected")
	ErrWatermarkViolation = errors.New("watermark violation")
)

type Window struct {
	VehicleID   string
	StartTimeNS int64
	EndTimeNS   int64
	States      []types.VehicleState
	WatermarkNS int64
	IsFinalized bool
}

type WindowPolicy struct {
	WindowSizeNS      int64
	SlideNS           int64
	AllowedLatenessNS int64
}

type StreamEvent struct {
	State       types.VehicleState
	ArrivalTimeNS int64
}

type WindowState struct {
	Active        map[int64]*Window
	Finalized     []*Window
	MaxObservedTime int64
	Watermark     int64
}

type WindowingEngine struct {
	policy  WindowPolicy
	state   *WindowState
	deadLetter []StreamEvent
}

func DefaultWindowPolicy() WindowPolicy {
	return WindowPolicy{
		WindowSizeNS:      60 * int64(1e9),
		SlideNS:           30 * int64(1e9),
		AllowedLatenessNS: 5 * int64(1e9),
	}
}

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

func (we *WindowingEngine) Ingest(event StreamEvent) error {
	if event.State.TimestampNS < we.state.Watermark - we.policy.AllowedLatenessNS {
		we.deadLetter = append(we.deadLetter, event)
		return ErrLateEvent
	}

	windowStart := AssignWindow(event.State, we.policy)

	window, exists := we.state.Active[windowStart]
	if !exists {
		window = &Window{
			VehicleID:   event.State.VehicleID,
			StartTimeNS: windowStart,
			EndTimeNS:   windowStart + we.policy.WindowSizeNS,
			States:      make([]types.VehicleState, 0),
			WatermarkNS: we.state.Watermark,
			IsFinalized: false,
		}
		we.state.Active[windowStart] = window
	}

	for _, existing := range window.States {
		if existing.VehicleID == event.State.VehicleID &&
			existing.TimestampNS == event.State.TimestampNS {
			return nil
		}
	}

	window.States = append(window.States, event.State)
	sort.Slice(window.States, func(i, j int) bool {
		return window.States[i].TimestampNS < window.States[j].TimestampNS
	})

	if event.State.TimestampNS > we.state.MaxObservedTime {
		we.state.MaxObservedTime = event.State.TimestampNS
		we.AdvanceWatermark(we.state.MaxObservedTime)
	}

	return nil
}

func (we *WindowingEngine) AdvanceWatermark(now int64) {
	newWatermark := now - we.policy.AllowedLatenessNS
	if newWatermark > we.state.Watermark {
		we.state.Watermark = newWatermark

		for windowStart, window := range we.state.Active {
			if window.EndTimeNS < we.state.Watermark {
				window.IsFinalized = true
				we.state.Finalized = append(we.state.Finalized, window)
				delete(we.state.Active, windowStart)
			}
		}
	}
}

func (we *WindowingEngine) Emit() ([]*Window, error) {
	result := make([]*Window, 0, len(we.state.Finalized))
	for _, window := range we.state.Finalized {
		result = append(result, window)
	}
	return result, nil
}

func (we *WindowingEngine) CurrentWatermark() int64 {
	return we.state.Watermark
}

func (we *WindowingEngine) Finalized() []*Window {
	return we.state.Finalized
}

func (we *WindowingEngine) GetDeadLetter() []StreamEvent {
	return we.deadLetter
}

func AssignWindow(v types.VehicleState, policy WindowPolicy) int64 {
	return (v.TimestampNS / policy.SlideNS) * policy.SlideNS
}

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

func (w *Window) Finalize() {
	w.IsFinalized = true
}

func (we *WindowingEngine) Reset() {
	we.state = &WindowState{
		Active:    make(map[int64]*Window),
		Finalized: make([]*Window, 0),
		Watermark: 0,
	}
	we.deadLetter = make([]StreamEvent, 0)
}
