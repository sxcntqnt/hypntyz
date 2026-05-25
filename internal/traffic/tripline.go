package traffic

import (
	"math"
)

// Point represents a geographic coordinate
type Point struct {
	Lat float64
	Lon float64
}

// TripLine represents a virtual perpendicular line across a road segment
// Used to detect crossings and compute speed samples
type TripLine struct {
	ID        int64
	SegmentID int64
	Index     int // 1=Entry, 2=Exit
	Dist      float64 // Distance from start of segment (meters)
	P1        Point   // Perpendicular point 1
	P2        Point   // Perpendicular point 2
	Bearing   float64 // Bearing of the road at this point
}

// Crossing represents a detected crossing of a TripLine by a vehicle
type Crossing struct {
	TripLine  *TripLine
	Time      int64 // Nanoseconds
	VehicleID string
}

// LineIntersection computes the intersection fraction (t) of two line segments
// Returns (t, u, intersects) where t is fraction along segment1, u along segment2
func LineIntersection(p1, p2, p3, p4 Point) (float64, float64, bool) {
	d := (p4.Lon-p3.Lon)*(p1.Lat-p2.Lat) - (p1.Lon-p2.Lon)*(p4.Lat-p3.Lat)
	if d == 0 {
		return 0, 0, false
	}

	t := ((p3.Lat-p4.Lat)*(p1.Lon-p3.Lon) + (p3.Lon-p4.Lon)*(p3.Lat-p1.Lat)) / d
	u := ((p1.Lat-p2.Lat)*(p1.Lon-p3.Lon) + (p1.Lon-p2.Lon)*(p3.Lat-p1.Lat)) / d

	return t, u, (t >= 0 && t <= 1 && u >= 0 && u <= 1)
}

// DistanceMeters computes distance between two points (Haversine approximation)
func DistanceMeters(p1, p2 Point) float64 {
	const R = 6371000 // Earth radius in meters
	dLat := (p2.Lat - p1.Lat) * math.Pi / 180.0
	dLon := (p2.Lon - p1.Lon) * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(p1.Lat*math.Pi/180.0)*math.Cos(p2.Lat*math.Pi/180.0)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// Bearing computes the bearing between two points (degrees)
func Bearing(p1, p2 Point) float64 {
	dLon := (p2.Lon - p1.Lon) * math.Pi / 180.0
	lat1 := p1.Lat * math.Pi / 180.0
	lat2 := p2.Lat * math.Pi / 180.0

	y := math.Sin(dLon) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dLon)
	bearing := math.Atan2(y, x) * 180.0 / math.Pi

	if bearing < 0 {
		bearing += 360
	}
	return bearing
}

// PerpendicularPoint computes a point perpendicular to a bearing at a given distance
func PerpendicularPoint(p Point, bearingDeg, distanceMeters float64) Point {
	const R = 6371000
	bearingRad := bearingDeg * math.Pi / 180.0
	lat1 := p.Lat * math.Pi / 180.0
	lon1 := p.Lon * math.Pi / 180.0

	lat2 := math.Asin(math.Sin(lat1)*math.Cos(distanceMeters/R) +
		math.Cos(lat1)*math.Sin(distanceMeters/R)*math.Cos(bearingRad))
	lon2 := lon1 + math.Atan2(math.Sin(bearingRad)*math.Sin(distanceMeters/R)*math.Cos(lat1),
		math.Cos(distanceMeters/R)-math.Sin(lat1)*math.Sin(lat2))

	return Point{
		Lat: lat2 * 180.0 / math.Pi,
		Lon: lon2 * 180.0 / math.Pi,
	}
}

// CreateTripLine creates a TripLine perpendicular to a road segment
func CreateTripLine(segmentID int64, index int, dist float64, p Point, bearing float64, widthMeters float64) *TripLine {
	// Create perpendicular points (widthMeters/2 on each side)
	halfWidth := widthMeters / 2.0
	// Perpendicular bearing = bearing + 90
	perpBearing := bearing + 90.0
	if perpBearing > 360 {
		perpBearing -= 360
	}

	p1 := PerpendicularPoint(p, perpBearing, halfWidth)
	p2 := PerpendicularPoint(p, perpBearing+180, halfWidth)

	return &TripLine{
		SegmentID: segmentID,
		Index:     index,
		Dist:      dist,
		P1:        p1,
		P2:        p2,
		Bearing:   bearing,
	}
}

// CheckCrossing checks if a GPS segment (p0 -> p1) crosses this TripLine
func (tl *TripLine) CheckCrossing(p0, p1 Point, t0, t1 int64) *Crossing {
	// Check intersection with TripLine segment (p1-p2)
	t, _, intersects := LineIntersection(p0, p1, tl.P1, tl.P2)

	if !intersects {
		return nil
	}

	// Interpolate time at crossing
	crossingTime := t0 + int64(float64(t1-t0)*t)

	return &Crossing{
		TripLine:  tl,
		Time:      crossingTime,
		VehicleID: "", // Will be filled by caller
	}
}

// SpeedSample represents a speed observation on a segment
type SpeedSample struct {
	SegmentID int64
	Time      int64
	Speed     float64 // m/s
}

// ComputeSpeed computes speed between two crossings
func ComputeSpeed(c1, c2 *Crossing) *SpeedSample {
	if c1.TripLine.SegmentID != c2.TripLine.SegmentID {
		return nil
	}

	// Must be entry -> exit (index 1 -> 2)
	if c1.TripLine.Index >= c2.TripLine.Index {
		return nil
	}

	dist := c2.TripLine.Dist - c1.TripLine.Dist
	if dist <= 0 {
		return nil
	}

	dt := float64(c2.Time - c1.Time) / 1e9 // Convert ns to seconds
	if dt <= 0 {
		return nil
	}

	speed := dist / dt

	// Filter unrealistic speeds (> 31 m/s ≈ 111 km/h)
	if speed > 31.0 || speed < 0 {
		return nil
	}

	return &SpeedSample{
		SegmentID: c1.TripLine.SegmentID,
		Time:      c1.Time,
		Speed:     speed,
	}
}
