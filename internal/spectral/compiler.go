package spectral

import "hypnotz/internal/types"

// SpectralFeatureCompiler sits between the Temporal Sequence Compiler and
// the Memory Engine in the pipeline. It extracts the speed signal from a
// TensorSequence, optionally merges it with the entity's historical speed
// ring buffer, and computes frequency-domain features.
//
// Pipeline position:
//
//	VehicleState[] → TensorSequence
//	                      ↓
//	          SpectralFeatureCompiler
//	                      ↓
//	          EnrichedSequence (time + frequency)
//	                      ↓
//	             Memory Engine
type SpectralFeatureCompiler struct {
	windowSize int     // number of samples per FFT frame
	minSamples int     // minimum samples required to attempt FFT
	sampleRate float64 // assumed Hz, used for frequency axis only
}

// NewSpectralFeatureCompiler returns a compiler with the given parameters.
// Use DefaultWindowSize, DefaultMinSamples, DefaultSampleRate for the
// standard realtime configuration.
func NewSpectralFeatureCompiler(windowSize, minSamples int, sampleRate float64) *SpectralFeatureCompiler {
	return &SpectralFeatureCompiler{
		windowSize: windowSize,
		minSamples: minSamples,
		sampleRate: sampleRate,
	}
}

// NewDefaultCompiler returns a compiler with production realtime defaults
// (N=32, min=16, 20 Hz).
func NewDefaultCompiler() *SpectralFeatureCompiler {
	return NewSpectralFeatureCompiler(DefaultWindowSize, DefaultMinSamples, DefaultSampleRate)
}

// Compile produces an EnrichedSequence from seq, optionally supplemented by
// speedHistory from the entity's ring buffer.
//
// Speed values are extracted from token index 2 of each TensorSequence token
// (the feature layout is [lat, lon, speed, heading, confidence, time_delta, source]).
// speedHistory, when non-nil, is prepended to the sequence speeds so that the
// FFT sees a longer continuous signal across multiple ticks.
//
// If the combined sample count is below minSamples the returned EnrichedSequence
// has Valid=false and zero-valued Spectral fields.
func (c *SpectralFeatureCompiler) Compile(seq types.TensorSequence, speedHistory []float64) EnrichedSequence {
	base := EnrichedSequence{Base: seq}

	// Extract speed signal from the token stream (feature index 2).
	seqSpeeds := extractSpeeds(seq.Tokens)

	// Merge history + current sequence speeds, then take the last windowSize.
	combined := merge(speedHistory, seqSpeeds, c.windowSize)

	if len(combined) < c.minSamples {
		// Not enough data — return base sequence without spectral features.
		return base
	}

	sp := Compute(combined, c.sampleRate)
	base.Spectral = Extract(sp)
	base.Valid = true
	return base
}

// CompileFromHistory produces an EnrichedSequence directly from a pre-built
// speed history slice (e.g. from an entity's RingBuffer). This path is used
// by the memory engine when updating a MemoryEntity's SpectralProfile outside
// of the normal compilation pipeline.
func (c *SpectralFeatureCompiler) CompileFromHistory(vehicleID string, history []float64) EnrichedSequence {
	base := EnrichedSequence{
		Base: types.TensorSequence{VehicleID: vehicleID},
	}
	window := tail(history, c.windowSize)
	if len(window) < c.minSamples {
		return base
	}
	sp := Compute(window, c.sampleRate)
	base.Spectral = Extract(sp)
	base.Valid = true
	return base
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

// extractSpeeds pulls the speed value (token index 2) from each token.
// Tokens shorter than 3 elements contribute a zero.
func extractSpeeds(tokens [][]float64) []float64 {
	out := make([]float64, len(tokens))
	for i, tok := range tokens {
		if len(tok) > 2 {
			out[i] = tok[2]
		}
	}
	return out
}

// merge concatenates history and current, then returns the last maxLen
// elements. If the total length is ≤ maxLen the result is history+current.
func merge(history, current []float64, maxLen int) []float64 {
	combined := make([]float64, 0, len(history)+len(current))
	combined = append(combined, history...)
	combined = append(combined, current...)
	return tail(combined, maxLen)
}

// tail returns the last n elements of s, or all of s when len(s) ≤ n.
func tail(s []float64, n int) []float64 {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
