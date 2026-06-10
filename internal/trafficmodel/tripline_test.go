package trafficmodel_test

import (
	"math"
	"testing"

	"hypnotz/internal/trafficmodel"
)

func TestLineIntersection(t *testing.T) {
	p0 := trafficmodel.Point{Lat: 0, Lon: 0}
	p1 := trafficmodel.Point{Lat: 0, Lon: 1}
	p2 := trafficmodel.Point{Lat: -0.5, Lon: 0.5}
	p3 := trafficmodel.Point{Lat: 0.5, Lon: 0.5}

	tVal, uVal, ok := trafficmodel.LineIntersection(p0, p1, p2, p3)
	if !ok {
		t.Fatal("lines should intersect")
	}
	if tVal != 0.5 {
		t.Errorf("expected t=0.5, got %f", tVal)
	}
	if uVal != 0.5 {
		t.Errorf("expected u=0.5, got %f", uVal)
	}

	p4 := trafficmodel.Point{Lat: 0, Lon: 0}
	p5 := trafficmodel.Point{Lat: 0, Lon: 1}
	p6 := trafficmodel.Point{Lat: 1, Lon: 0}
	p7 := trafficmodel.Point{Lat: 1, Lon: 1}
	if _, _, ok = trafficmodel.LineIntersection(p4, p5, p6, p7); ok {
		t.Error("parallel lines must not intersect")
	}
}

func TestDistanceMeters(t *testing.T) {
	p1 := trafficmodel.Point{Lat: 0, Lon: 0}
	p2 := trafficmodel.Point{Lat: 1, Lon: 0}
	dist := trafficmodel.DistanceMeters(p1, p2)
	if dist < 110_000 || dist > 112_000 {
		t.Errorf("expected ~111 km, got %.0f m", dist)
	}
}

func TestCreateTripLine(t *testing.T) {
	p := trafficmodel.Point{Lat: 37.7749, Lon: -122.4194}
	tl := trafficmodel.CreateTripLine(42, 1, 1, 20.0, p, 0.0, 20.0)

	if tl.ID != 42 {
		t.Errorf("ID: got %d, want 42", tl.ID)
	}
	if tl.SegmentID != 1 {
		t.Error("SegmentID mismatch")
	}
	if tl.Index != 1 {
		t.Error("Index mismatch")
	}
	if tl.Dist != 20.0 {
		t.Error("Dist mismatch")
	}
}

func TestCheckCrossing(t *testing.T) {
	p := trafficmodel.Point{Lat: 0, Lon: 0}
	tl := trafficmodel.CreateTripLine(1, 1, 1, 10.0, p, 0.0, 20.0)

	gpsP0 := trafficmodel.Point{Lat: -0.0001, Lon: 0}
	gpsP1 := trafficmodel.Point{Lat: 0.0001, Lon: 0}

	crossing := tl.CheckCrossing(gpsP0, gpsP1, 0, 1_000_000_000)
	if crossing == nil {
		t.Fatal("should detect crossing")
	}
	if crossing.TripLine != tl {
		t.Error("crossing should reference the originating trip line")
	}
}

func TestComputeSpeed(t *testing.T) {
	p := trafficmodel.Point{Lat: 0, Lon: 0}
	tl1 := trafficmodel.CreateTripLine(1, 1, 1, 0.0, p, 0.0, 20.0)
	tl2 := trafficmodel.CreateTripLine(2, 1, 2, 100.0, p, 0.0, 20.0)

	c1 := &trafficmodel.Crossing{TripLine: tl1, Time: 0}
	c2 := &trafficmodel.Crossing{TripLine: tl2, Time: 5_000_000_000}

	sample := trafficmodel.ComputeSpeed(c1, c2)
	if sample == nil {
		t.Fatal("should compute a speed sample")
	}
	if math.Abs(sample.Speed-20.0) > 0.1 {
		t.Errorf("expected 20 m/s, got %.2f", sample.Speed)
	}
}

func TestSpeedSampleSpectralDeviationDefaultsZero(t *testing.T) {
	// ComputeSpeed must return a sample with SpectralDeviationScore = 0.
	// The memory layer is responsible for populating it; trafficmodel must
	// not assume spectral context is available.
	p := trafficmodel.Point{Lat: 0, Lon: 0}
	tl1 := trafficmodel.CreateTripLine(1, 1, 1, 0.0, p, 0.0, 20.0)
	tl2 := trafficmodel.CreateTripLine(2, 1, 2, 50.0, p, 0.0, 20.0)

	c1 := &trafficmodel.Crossing{TripLine: tl1, Time: 0}
	c2 := &trafficmodel.Crossing{TripLine: tl2, Time: 2_500_000_000}

	sample := trafficmodel.ComputeSpeed(c1, c2)
	if sample == nil {
		t.Fatal("should compute a speed sample")
	}
	if sample.SpectralDeviationScore != 0.0 {
		t.Errorf("SpectralDeviationScore should default to 0, got %f",
			sample.SpectralDeviationScore)
	}
}

func TestIsSpectrallyAnomalous(t *testing.T) {
	sample := &trafficmodel.SpeedSample{
		SegmentID:              1,
		Speed:                  20.0,
		SpectralDeviationScore: 0.75,
	}
	if !sample.IsSpectrallyAnomalous(0.6) {
		t.Error("score 0.75 should be anomalous above threshold 0.6")
	}
	if sample.IsSpectrallyAnomalous(0.8) {
		t.Error("score 0.75 should not be anomalous above threshold 0.8")
	}
}

func TestIsSpectrallyAnomalousZeroScore(t *testing.T) {
	sample := &trafficmodel.SpeedSample{Speed: 15.0}
	if sample.IsSpectrallyAnomalous(0.6) {
		t.Error("zero SpectralDeviationScore should never be anomalous")
	}
}

func TestSpeedHistogram(t *testing.T) {
	h := trafficmodel.NewSpeedHistogram()
	for i := 0; i < 100; i++ {
		h.AddSample(int64(i)*int64(1e9), 15.0)
	}
	_, mean, _ := h.GetStats()
	if mean < 14.0 || mean > 16.0 {
		t.Errorf("expected mean ≈ 15 m/s, got %.2f", mean)
	}
}

func TestSpeedHistogramBinning(t *testing.T) {
	h := trafficmodel.NewSpeedHistogram()
	h.AddSample(10*3600*int64(1e9), 13.89)
	key := trafficmodel.PackBin(10, 50)
	bins := h.ExportBins()
	if bins[key] != 1 {
		t.Errorf("expected bin[%d]=1, got %d", key, bins[key])
	}
}

func TestSpeedDeviation(t *testing.T) {
	h := trafficmodel.NewSpeedHistogram()
	for i := 0; i < 100; i++ {
		h.AddSample(int64(i)*int64(1e9), 20.0)
	}
	_, mean, stddev := h.GetStats()
	if mean < 19.0 || mean > 21.0 {
		t.Errorf("expected mean ≈ 20 m/s, got %.2f", mean)
	}
	if stddev > 1.0 {
		t.Errorf("expected low stddev for uniform samples, got %.4f", stddev)
	}
}
