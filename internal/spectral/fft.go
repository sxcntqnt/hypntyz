package spectral

import (
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

const (
	// DefaultWindowSize is the number of samples per FFT frame.
	// Must be a power of 2 for efficient computation. At 20 Hz this
	// covers a 1.6-second realtime window.
	DefaultWindowSize = 32

	// DefaultMinSamples is the minimum number of samples required before
	// the spectral compiler will attempt a transform. Below this threshold
	// the output is marked invalid and all features are zero.
	DefaultMinSamples = 16

	// DefaultSampleRate is the assumed sampling frequency in Hz.
	// Matches the engine tick rate (20 Hz → 50 ms per tick).
	DefaultSampleRate = 20.0
)

// Spectrum holds the output of a single FFT frame.
type Spectrum struct {
	// Magnitudes[i] is the amplitude at Freqs[i].
	// Length is WindowSize/2 + 1 (DC through Nyquist, inclusive).
	Magnitudes []float64

	// Freqs[i] is the frequency in Hz corresponding to Magnitudes[i].
	Freqs []float64

	// SampleRate used when computing this spectrum.
	SampleRate float64
}

// Compute runs a Hann-windowed real FFT over samples and returns the
// one-sided magnitude spectrum. sampleRate is used only to populate the
// frequency axis; it does not affect the magnitudes.
//
// len(samples) must be ≥ 2. Callers should ensure len(samples) is a
// power of 2 for optimal gonum performance.
func Compute(samples []float64, sampleRate float64) Spectrum {
	n := len(samples)
	if n < 2 {
		return Spectrum{SampleRate: sampleRate}
	}

	// Apply Hann window to reduce spectral leakage at frame boundaries.
	windowed := applyHann(samples)

	// Forward real FFT → complex coefficients [0 .. n/2].
	ft := fourier.NewFFT(n)
	coeffs := ft.Coefficients(nil, windowed)

	numBins := len(coeffs) // n/2 + 1
	mags := make([]float64, numBins)
	freqs := make([]float64, numBins)

	for i, c := range coeffs {
		re, im := real(c), imag(c)
		mags[i] = math.Sqrt(re*re + im*im)
		freqs[i] = float64(i) * sampleRate / float64(n)
	}

	return Spectrum{
		Magnitudes: mags,
		Freqs:      freqs,
		SampleRate: sampleRate,
	}
}

// ComputeAcceleration derives a first-difference acceleration signal from
// speed samples, assuming uniform sampling at sampleRate Hz, then computes
// its FFT spectrum. This complements the speed spectrum for detecting
// rapid velocity changes that are not visible in the speed magnitude alone.
func ComputeAcceleration(speeds []float64, sampleRate float64) Spectrum {
	if len(speeds) < 2 {
		return Spectrum{SampleRate: sampleRate}
	}
	dt := 1.0 / sampleRate
	accel := make([]float64, len(speeds)-1)
	for i := 1; i < len(speeds); i++ {
		accel[i-1] = (speeds[i] - speeds[i-1]) / dt
	}
	return Compute(accel, sampleRate)
}

// applyHann multiplies samples by the Hann window function and returns
// the result as a new slice.
func applyHann(samples []float64) []float64 {
	n := len(samples)
	out := make([]float64, n)
	// w[i] = 0.5 * (1 − cos(2π·i / (N−1)))
	denom := float64(n - 1)
	for i, s := range samples {
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/denom))
		out[i] = s * w
	}
	return out
}
