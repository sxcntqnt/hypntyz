package spectral_test

import (
	"math"
	"testing"

	"hypnotz/internal/spectral"
	"hypnotz/internal/types"
)

// ─── RingBuffer ────────────────────────────────────────────────────────────────

func TestRingBufferEmpty(t *testing.T) {
	r := spectral.NewRingBuffer(8)
	if r.Len() != 0 {
		t.Errorf("new buffer should have len 0, got %d", r.Len())
	}
	if r.Full() {
		t.Error("new buffer should not be full")
	}
	if r.Slice() != nil {
		t.Error("empty buffer Slice should return nil")
	}
}

func TestRingBufferPartialFill(t *testing.T) {
	r := spectral.NewRingBuffer(8)
	r.Push(1.0)
	r.Push(2.0)
	r.Push(3.0)

	if r.Len() != 3 {
		t.Errorf("expected len 3, got %d", r.Len())
	}
	s := r.Slice()
	if len(s) != 3 || s[0] != 1.0 || s[1] != 2.0 || s[2] != 3.0 {
		t.Errorf("unexpected slice contents: %v", s)
	}
}

func TestRingBufferWraparound(t *testing.T) {
	r := spectral.NewRingBuffer(4)
	for i := 1; i <= 6; i++ {
		r.Push(float64(i))
	}
	// After pushing 1,2,3,4,5,6 into a size-4 buffer:
	// oldest → newest should be 3,4,5,6
	s := r.Slice()
	if len(s) != 4 {
		t.Fatalf("expected 4 elements, got %d", len(s))
	}
	expected := []float64{3, 4, 5, 6}
	for i, v := range expected {
		if s[i] != v {
			t.Errorf("s[%d]: expected %f, got %f", i, v, s[i])
		}
	}
}

func TestRingBufferReset(t *testing.T) {
	r := spectral.NewRingBuffer(4)
	r.Push(1.0)
	r.Push(2.0)
	r.Reset()
	if r.Len() != 0 {
		t.Errorf("after reset len should be 0, got %d", r.Len())
	}
	if r.Slice() != nil {
		t.Error("after reset Slice should return nil")
	}
}

// ─── FFT / Spectrum ────────────────────────────────────────────────────────────

func TestComputeReturnsBins(t *testing.T) {
	samples := make([]float64, 32)
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	// n/2 + 1 = 17 bins
	if len(sp.Magnitudes) != 17 {
		t.Errorf("expected 17 magnitude bins for n=32, got %d", len(sp.Magnitudes))
	}
	if len(sp.Freqs) != len(sp.Magnitudes) {
		t.Error("Freqs and Magnitudes must have equal length")
	}
}

func TestComputeDCComponent(t *testing.T) {
	// A constant signal has all energy at DC (bin 0).
	n := 32
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = 5.0
	}
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	// DC bin should dominate; all others should be near zero.
	if sp.Magnitudes[0] == 0 {
		t.Error("DC component should be non-zero for constant signal")
	}
}

func TestComputeSingleFrequency(t *testing.T) {
	// A pure sine at exactly the bin-1 frequency should produce a
	// clear peak at bin 1 (after Hann windowing, most energy there).
	n := 32
	sampleRate := float64(spectral.DefaultSampleRate)
	freq := sampleRate / float64(n) // bin-1 frequency
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * freq * float64(i) / sampleRate)
	}
	sp := spectral.Compute(samples, sampleRate)
	// Bin 1 should have the largest magnitude (excluding DC).
	maxBin := 1
	for i := 2; i < len(sp.Magnitudes); i++ {
		if sp.Magnitudes[i] > sp.Magnitudes[maxBin] {
			maxBin = i
		}
	}
	if maxBin != 1 {
		t.Errorf("expected dominant bin=1 for single-frequency signal, got %d", maxBin)
	}
}

func TestComputeShortInputReturnsEmpty(t *testing.T) {
	sp := spectral.Compute([]float64{1.0}, spectral.DefaultSampleRate)
	if len(sp.Magnitudes) != 0 {
		t.Error("single-sample input should return empty spectrum")
	}
}

func TestFrequencyAxisZeroBin(t *testing.T) {
	samples := make([]float64, 32)
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	if sp.Freqs[0] != 0.0 {
		t.Errorf("bin 0 frequency should be 0 Hz, got %f", sp.Freqs[0])
	}
}

func TestFrequencyAxisNyquist(t *testing.T) {
	n := 32
	sr := float64(spectral.DefaultSampleRate)
	samples := make([]float64, n)
	sp := spectral.Compute(samples, sr)
	nyquist := sr / 2.0
	last := sp.Freqs[len(sp.Freqs)-1]
	if math.Abs(last-nyquist) > 1e-9 {
		t.Errorf("last bin should be Nyquist (%.2f Hz), got %.6f", nyquist, last)
	}
}

// ─── Feature extraction ────────────────────────────────────────────────────────

func TestExtractEmptySpectrum(t *testing.T) {
	f := spectral.Extract(spectral.Spectrum{})
	if f.SpectralEntropy != 0 || f.CoherenceScore != 0 {
		t.Error("empty spectrum should yield zero features")
	}
}

func TestExtractBandEnergiesSumToOne(t *testing.T) {
	// White-noise-like signal: uniform random-ish values.
	samples := make([]float64, 32)
	for i := range samples {
		samples[i] = float64(i%7) * 0.1 // deterministic pseudo-random
	}
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	f := spectral.Extract(sp)

	sum := f.LowBandEnergy + f.MidBandEnergy + f.HighBandEnergy
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("band energies should sum to 1.0, got %.10f", sum)
	}
}

func TestExtractEntropyBounds(t *testing.T) {
	samples := make([]float64, 32)
	for i := range samples {
		samples[i] = float64(i) * 0.3
	}
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	f := spectral.Extract(sp)

	if f.SpectralEntropy < 0 || f.SpectralEntropy > 1.0001 {
		t.Errorf("spectral entropy out of [0,1]: %f", f.SpectralEntropy)
	}
}

func TestExtractCoherenceBounds(t *testing.T) {
	samples := make([]float64, 32)
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * float64(i) / 32)
	}
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	f := spectral.Extract(sp)

	if f.CoherenceScore < 0 || f.CoherenceScore > 1.0001 {
		t.Errorf("coherence score out of [0,1]: %f", f.CoherenceScore)
	}
}

func TestExtractHighEntropyForNoise(t *testing.T) {
	// Irregular signal should have higher entropy than a pure sine.
	sineWave := make([]float64, 32)
	irregular := make([]float64, 32)
	for i := range sineWave {
		sineWave[i] = math.Sin(2 * math.Pi * float64(i) / 32)
		irregular[i] = math.Sin(float64(i)*1.3) + math.Cos(float64(i)*2.7) +
			math.Sin(float64(i)*5.1)
	}
	spSine := spectral.Compute(sineWave, spectral.DefaultSampleRate)
	spIrr := spectral.Compute(irregular, spectral.DefaultSampleRate)
	fSine := spectral.Extract(spSine)
	fIrr := spectral.Extract(spIrr)

	if fIrr.SpectralEntropy <= fSine.SpectralEntropy {
		t.Errorf("irregular signal should have higher entropy (%.4f) than sine (%.4f)",
			fIrr.SpectralEntropy, fSine.SpectralEntropy)
	}
}

func TestAnomalyScoreBounds(t *testing.T) {
	samples := make([]float64, 32)
	for i := range samples {
		samples[i] = float64(i % 5)
	}
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	f := spectral.Extract(sp)
	score := spectral.ComputeAnomalyScore(f)

	if score < 0 || score > 1 {
		t.Errorf("anomaly score out of [0,1]: %f", score)
	}
}

// ─── SpectralProfile ───────────────────────────────────────────────────────────

func TestSpectralProfileUpdate(t *testing.T) {
	p := spectral.NewSpectralProfile()
	samples := make([]float64, 32)
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * float64(i) / 32)
	}
	sp := spectral.Compute(samples, spectral.DefaultSampleRate)
	p.Update(sp, 1_000_000_000)

	if p.LastUpdatedNS != 1_000_000_000 {
		t.Error("LastUpdatedNS not set correctly")
	}
	if len(p.EntropyHistory) != 1 {
		t.Errorf("expected 1 entropy history entry, got %d", len(p.EntropyHistory))
	}
}

func TestSpectralProfileEntropySpikeInsufficientHistory(t *testing.T) {
	p := spectral.NewSpectralProfile()
	// With < 3 entries, EntropySpike should always return false.
	if p.EntropySpike(2.0) {
		t.Error("should not detect spike with empty history")
	}
}

func TestSpectralProfileEntropySpike(t *testing.T) {
	p := spectral.NewSpectralProfile()
	// Populate with stable low-entropy values, then inject a spike.
	for i := 0; i < 8; i++ {
		samples := make([]float64, 32)
		for j := range samples {
			samples[j] = math.Sin(2 * math.Pi * float64(j) / 32)
		}
		sp := spectral.Compute(samples, spectral.DefaultSampleRate)
		p.Update(sp, int64(i)*1_000_000_000)
	}
	// Inject high-entropy (noisy) sample.
	noisy := make([]float64, 32)
	for i := range noisy {
		noisy[i] = math.Sin(float64(i)*1.1) + math.Cos(float64(i)*3.7) +
			math.Sin(float64(i)*7.3) + math.Cos(float64(i)*11.9)
	}
	spNoisy := spectral.Compute(noisy, spectral.DefaultSampleRate)
	p.Update(spNoisy, 9_000_000_000)

	if !p.EntropySpike(1.5) {
		t.Error("expected entropy spike to be detected after injecting noisy sample")
	}
}

// ─── SpectralFeatureCompiler ───────────────────────────────────────────────────

func TestCompilerInvalidBelowMinSamples(t *testing.T) {
	c := spectral.NewDefaultCompiler()
	// Sequence with only 4 tokens — below DefaultMinSamples (16).
	seq := types.TensorSequence{
		VehicleID: "v1",
		Tokens:    make([][]float64, 4),
	}
	for i := range seq.Tokens {
		seq.Tokens[i] = make([]float64, 7)
	}
	out := c.Compile(seq, nil)
	if out.Valid {
		t.Error("compiler should mark output invalid for < minSamples tokens")
	}
}

func TestCompilerValidWithEnoughTokens(t *testing.T) {
	c := spectral.NewDefaultCompiler()
	seq := types.TensorSequence{
		VehicleID: "v1",
		Tokens:    make([][]float64, 32),
	}
	for i := range seq.Tokens {
		seq.Tokens[i] = make([]float64, 7)
		seq.Tokens[i][2] = 15.0 + float64(i%5)*0.5 // speed varies
	}
	out := c.Compile(seq, nil)
	if !out.Valid {
		t.Error("compiler should produce valid output for ≥ minSamples tokens")
	}
}

func TestCompilerHistoryMerge(t *testing.T) {
	c := spectral.NewSpectralFeatureCompiler(32, 16, spectral.DefaultSampleRate)
	// Sequence has only 8 tokens (below min), but history has 24 samples.
	history := make([]float64, 24)
	for i := range history {
		history[i] = 20.0
	}
	seq := types.TensorSequence{
		VehicleID: "v1",
		Tokens:    make([][]float64, 8),
	}
	for i := range seq.Tokens {
		seq.Tokens[i] = []float64{0, 0, 20.0, 0, 0, 0, 0}
	}
	out := c.Compile(seq, history)
	if !out.Valid {
		t.Error("compiler should be valid when history + tokens ≥ minSamples")
	}
}

func TestCompilerBaseSequencePreserved(t *testing.T) {
	c := spectral.NewDefaultCompiler()
	seq := types.TensorSequence{
		VehicleID:    "v42",
		Tokens:       make([][]float64, 32),
		IsTimeSorted: true,
	}
	for i := range seq.Tokens {
		seq.Tokens[i] = make([]float64, 7)
	}
	out := c.Compile(seq, nil)
	if out.Base.VehicleID != "v42" {
		t.Errorf("base VehicleID not preserved: got %q", out.Base.VehicleID)
	}
	if !out.Base.IsTimeSorted {
		t.Error("base IsTimeSorted not preserved")
	}
}

func TestCompileFromHistory(t *testing.T) {
	c := spectral.NewDefaultCompiler()
	history := make([]float64, 32)
	for i := range history {
		history[i] = 18.0 + float64(i%3)*0.2
	}
	out := c.CompileFromHistory("v1", history)
	if !out.Valid {
		t.Error("CompileFromHistory should be valid with 32 samples")
	}
}
