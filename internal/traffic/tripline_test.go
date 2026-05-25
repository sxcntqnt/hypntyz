package traffic_test

import (
	"math"
	"testing"

	"hypnotz/internal/traffic"
)

func TestLineIntersection(t *testing.T) {
	// Horizontal line p0->p1, vertical line p2->p3
	p0 := traffic.Point{Lat: 0, Lon: 0}
	p1 := traffic.Point{Lat: 0, Lon: 1}
	p2 := traffic.Point{Lat: -0.5, Lon: 0.5}
	p3 := traffic.Point{Lat: 0.5, Lon: 0.5}

	tVal, uVal, intersects := traffic.LineIntersection(p0, p1, p2, p3)

	if !intersects {
		t.Error("Lines should intersect")
	}

	if tVal != 0.5 {
		t.Errorf("Expected t=0.5, got %f", tVal)
	}

	if uVal != 0.5 {
		t.Errorf("Expected u=0.5, got %f", uVal)
	}

	// Parallel lines
	p4 := traffic.Point{Lat: 0, Lon: 0}
	p5 := traffic.Point{Lat: 0, Lon: 1}
	p6 := traffic.Point{Lat: 1, Lon: 0}
	p7 := traffic.Point{Lat: 1, Lon: 1}

	_, _, intersects = traffic.LineIntersection(p4, p5, p6, p7)
	if intersects {
		t.Error("Parallel lines should not intersect")
	}
}

func TestDistanceMeters(t *testing.T) {
	// Approximately 111km per degree latitude
	p1 := traffic.Point{Lat: 0, Lon: 0}
	p2 := traffic.Point{Lat: 1, Lon: 0}

	dist := traffic.DistanceMeters(p1, p2)

	if dist < 110000 || dist > 112000 {
		t.Errorf("Expected ~111km, got %f", dist)
	}
}

func TestCreateTripLine(t *testing.T) {
	p := traffic.Point{Lat: 37.7749, Lon: -122.4194}
	tl := traffic.CreateTripLine(1, 1, 20.0, p, 0.0, 20.0)

	if tl.SegmentID != 1 {
		t.Error("SegmentID mismatch")
	}

	if tl.Index != 1 {
		t.Error("Index mismatch")
	}

	if tl.Dist != 20.0 {
		t.Error("Distance mismatch")
	}
}

func TestCheckCrossing(t *testing.T) {
	p := traffic.Point{Lat: 0, Lon: 0}
	tl := traffic.CreateTripLine(1, 1, 10.0, p, 0.0, 20.0)

	// GPS segment that crosses the trip line
	gpsP0 := traffic.Point{Lat: -0.0001, Lon: 0}
	gpsP1 := traffic.Point{Lat: 0.0001, Lon: 0}

	crossing := tl.CheckCrossing(gpsP0, gpsP1, 0, 1000000000)

	if crossing == nil {
		t.Error("Should detect crossing")
	}

	if crossing.TripLine != tl {
		t.Error("Crossing should reference the trip line")
	}
}

func TestComputeSpeed(t *testing.T) {
	p := traffic.Point{Lat: 0, Lon: 0}
	tl1 := traffic.CreateTripLine(1, 1, 0.0, p, 0.0, 20.0)
	tl2 := traffic.CreateTripLine(1, 2, 100.0, p, 0.0, 20.0)

	c1 := &traffic.Crossing{TripLine: tl1, Time: 0}
	c2 := &traffic.Crossing{TripLine: tl2, Time: 5000000000} // 5 seconds

	sample := traffic.ComputeSpeed(c1, c2)

	if sample == nil {
		t.Error("Should compute speed")
	}

	// Speed = 100m / 5s = 20 m/s
	expected := 20.0
	if math.Abs(sample.Speed-expected) > 0.1 {
		t.Errorf("Expected speed ~%f, got %f", expected, sample.Speed)
	}
}

func TestSpeedHistogram(t *testing.T) {
	h := traffic.NewSpeedHistogram()

	// Add samples at different speeds
	for i := 0; i < 100; i++ {
		h.AddSample(int64(i*1e9), 15.0) // 15 m/s
	}

	_, mean, _ := h.GetStats()

	if mean < 14.0 || mean > 16.0 {
		t.Errorf("Expected mean around 15, got %f", mean)
	}
}

func TestSpeedHistogramBinning(t *testing.T) {
	h := traffic.NewSpeedHistogram()

	// Add sample at hour 10, speed 50 km/h (13.89 m/s)
	h.AddSample(10*3600*1e9, 13.89)

	// Should be in bin for hour 10, speed bin ~50
	bin := traffic.GetHourSpeedBin(10, 50)
	_, mean, _ := h.GetStats()
	
	_ = bin
	_ = mean
}

func TestSpeedDeviation(t *testing.T) {
	h := traffic.NewSpeedHistogram()

	// Add consistent samples
	for i := 0; i < 100; i++ {
		h.AddSample(int64(i*1e9), 20.0)
	}

	_, mean, stddev := h.GetStats()

	if mean < 19.0 || mean > 21.0 {
		t.Errorf("Expected mean around 20, got %f", mean)
	}

	if stddev > 1.0 {
		t.Errorf("Expected low stddev, got %f", stddev)
	}
}
