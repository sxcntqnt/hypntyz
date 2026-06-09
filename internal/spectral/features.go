package spectral

import (
	"math"

	"hypnotz/internal/types"
)

// SpectralFeatures holds the frequency-domain summary of one FFT frame.
// All values are normalised to [0, 1] unless stated otherwise.
type SpectralFeatures struct {
	// LowBandEnergy is the fraction of total spectral energy in the lowest
	// quarter of bins (slow trends — gradual acceleration / deceleration).
	LowBandEnergy float64

	// MidBandEnergy is the fraction of total spectral energy in the middle
	// quarter of bins (medium oscillations — traffic-signal-scale patterns).
	MidBandEnergy float64

	// HighBandEnergy is the fraction of total spectral energy in the upper
	// half of bins (rapid speed fluctuations, jitter, hard braking).
	HighBandEnergy float64

	// DominantFrequency is the frequency (Hz) of the bin with the largest
	// magnitude, excluding the DC component.
	DominantFrequency float64

	// SpectralEntropy is the normalised Shannon entropy of the power
	// distribution across all non-DC bins. Near 0 → one dominant frequency;
	// near 1 → energy spread uniformly (noise / unpredictable behaviour).
	SpectralEntropy float64

	// CoherenceScore is the ratio of the dominant bin's energy to the total
	// spectral energy. Near 1 → highly periodic; near 0 → incoherent.
	CoherenceScore float64
}

// SpectralProfile is the persistent frequency-domain fingerprint attached to
// a MemoryEntity. It is updated in-place on every tick where the entity's
// speed ring buffer has enough samples.
type SpectralProfile struct {
	// Features is the most recently computed SpectralFeatures frame.
	Features SpectralFeatures

	// RouteSignature stores the top-N FFT magnitude coefficients as a
	// compressed route fingerprint (float32 to halve memory cost).
	// Length is DefaultWindowSize/2 + 1.
	RouteSignature []float32

	// EntropyHistory is a rolling window of the last 10 SpectralEntropy
	// values, enabling entropy-spike detection across ticks.
	EntropyHistory []float32

	// AnomalyScore is the current spectral anomaly score in [0, 1].
	// Derived from HighBandEnergy and SpectralEntropy.
	AnomalyScore float32

	// LastUpdatedNS is the Unix nanosecond timestamp of the most recent
	// feature computation.
	LastUpdatedNS int64
}

// EnrichedSequence wraps a TensorSequence with frequency-domain features
// computed from the speed signal in its token stream. Used as the output
// of SpectralFeatureCompiler.
type EnrichedSequence struct {
	// Base is the original temporal sequence from the compiler stage.
	Base types.TensorSequence

	// Spectral holds the FFT features for the speed signal in this sequence.
	Spectral SpectralFeatures

	// Valid is false when the sequence did not contain enough samples to
	// produce a meaningful transform (< DefaultMinSamples tokens). When
	// false, Spectral is zero-valued and should not be used for scoring.
	Valid bool
}

// NewSpectralProfile allocates an empty SpectralProfile.
func NewSpectralProfile() *SpectralProfile {
	return &SpectralProfile{
		RouteSignature: make([]float32, DefaultWindowSize/2+1),
		EntropyHistory: make([]float32, 0, 10),
	}
}

// Update recomputes Features, RouteSignature, and AnomalyScore from sp.
// Appends the new entropy value to EntropyHistory (capped at 10 entries).
// now is the current Unix nanosecond timestamp.
func (p *SpectralProfile) Update(sp Spectrum, now int64) {
	p.Features = Extract(sp)
	p.AnomalyScore = ComputeAnomalyScore(p.Features)
	p.LastUpdatedNS = now

	// Store compressed route signature (magnitude coefficients as float32).
	sig := p.RouteSignature
	for i := range sig {
		if i < len(sp.Magnitudes) {
			sig[i] = float32(sp.Magnitudes[i])
		}
	}

	// Rolling entropy history (cap at 10).
	if len(p.EntropyHistory) >= 10 {
		p.EntropyHistory = p.EntropyHistory[1:]
	}
	p.EntropyHistory = append(p.EntropyHistory, float32(p.Features.SpectralEntropy))
}

// EntropySpike reports whether the most recent entropy value exceeds the
// rolling mean by more than threshold standard deviations. Returns false
// when there is insufficient history (< 3 entries).
func (p *SpectralProfile) EntropySpike(thresholdSigma float64) bool {
	n := len(p.EntropyHistory)
	if n < 3 {
		return false
	}
	var sum float64
	for _, v := range p.EntropyHistory {
		sum += float64(v)
	}
	mean := sum / float64(n)

	var variance float64
	for _, v := range p.EntropyHistory {
		d := float64(v) - mean
		variance += d * d
	}
	stddev := math.Sqrt(variance / float64(n))
	if stddev == 0 {
		return false
	}

	latest := float64(p.EntropyHistory[n-1])
	return (latest-mean)/stddev > thresholdSigma
}

// Extract computes SpectralFeatures from a Spectrum. The DC component
// (bin 0) is excluded from all band energy calculations.
func Extract(s Spectrum) SpectralFeatures {
	n := len(s.Magnitudes)
	if n <= 1 {
		return SpectralFeatures{}
	}

	// Band boundaries (DC-excluded bins 1..n-1):
	//   Low:  bins 1 .. n/4           (lowest quarter)
	//   Mid:  bins n/4+1 .. n/2       (middle quarter)
	//   High: bins n/2+1 .. n-1       (upper half)
	lowEnd := n / 4
	midEnd := n / 2

	var totalEnergy, lowEnergy, midEnergy, highEnergy float64
	var dominantMag float64
	dominantBin := 1

	for i := 1; i < n; i++ {
		e := s.Magnitudes[i] * s.Magnitudes[i]
		totalEnergy += e
		switch {
		case i <= lowEnd:
			lowEnergy += e
		case i <= midEnd:
			midEnergy += e
		default:
			highEnergy += e
		}
		if s.Magnitudes[i] > dominantMag {
			dominantMag = s.Magnitudes[i]
			dominantBin = i
		}
	}

	if totalEnergy == 0 {
		return SpectralFeatures{}
	}

	// Normalised band energies sum to 1.
	lowEnergy /= totalEnergy
	midEnergy /= totalEnergy
	highEnergy /= totalEnergy

	// Dominant frequency in Hz.
	dominantFreq := s.Freqs[dominantBin]

	// Normalised Shannon entropy of the power distribution.
	entropy := 0.0
	for i := 1; i < n; i++ {
		p := (s.Magnitudes[i] * s.Magnitudes[i]) / totalEnergy
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	maxEntropy := math.Log2(float64(n - 1)) // uniform distribution ceiling
	if maxEntropy > 0 {
		entropy /= maxEntropy
	}

	// Coherence: fraction of total energy held by the dominant bin.
	coherence := (dominantMag * dominantMag) / totalEnergy

	return SpectralFeatures{
		LowBandEnergy:     lowEnergy,
		MidBandEnergy:     midEnergy,
		HighBandEnergy:    highEnergy,
		DominantFrequency: dominantFreq,
		SpectralEntropy:   entropy,
		CoherenceScore:    coherence,
	}
}

// ComputeAnomalyScore derives a scalar [0, 1] anomaly signal from f.
// High-frequency energy and high entropy both indicate erratic behaviour;
// we weight them equally and clamp the result.
func ComputeAnomalyScore(f SpectralFeatures) float32 {
	score := 0.5*f.HighBandEnergy + 0.5*f.SpectralEntropy
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}
	return float32(score)
}
