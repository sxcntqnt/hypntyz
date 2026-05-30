// Package trafficmodel contains the pure data types and computation for
// TripLine-based speed sampling. It has no dependencies on other internal
// packages, which allows both memory and traffic to import it without cycles.
package trafficmodel

import "math"

// Point is a geographic coordinate in decimal degrees.
type Point struct {
	Lat float64
	Lon float64
}

// TripLine is a virtual perpendicular line drawn across a road segment.
// Vehicles crossing it (entry then exit) yield a SpeedSample.
type TripLine struct {
	ID        int64
	SegmentID int64
	Index     int     // 1 = entry, 2 = exit
	Dist      float64 // Distance from segment start, metres
	P1        Point   // Left perpendicular endpoint
	P2        Point   // Right perpendicular endpoint
	Bearing   float64 // Road bearing at this point, degrees
}

// Crossing records a single detected TripLine crossing by a vehicle.
type Crossing struct {
	TripLine  *TripLine
	Time      int64  // Unix nanoseconds
	VehicleID string
}

// SpeedSample is a derived speed observation on a road segment.
type SpeedSample struct {
	SegmentID int64
	Time      int64   // Unix nanoseconds, taken at the entry crossing
	Speed     float64 // m/s
}

const (
	earthRadiusMeters = 6_371_000.0

	// MaxRealisticSpeedMS is the upper bound for a plausible vehicle speed.
	// Anything faster (> ~111 km/h on a monitored segment) is treated as a
	// GPS artefact and discarded.
	MaxRealisticSpeedMS = 31.0 // m/s ≈ 111 km/h
)

// CreateTripLine constructs a TripLine perpendicular to the road at point p.
// id is a unique identifier; segmentID and index tie it to the road network.
// widthMeters is the full perpendicular span (half on each side).
func CreateTripLine(id, segmentID int64, index int, dist float64, p Point, bearing, widthMeters float64) *TripLine {
	halfWidth := widthMeters / 2.0
	perpBearing := normalizeBearing(bearing + 90.0)
	return &TripLine{
		ID:        id,
		SegmentID: segmentID,
		Index:     index,
		Dist:      dist,
		P1:        PerpendicularPoint(p, perpBearing, halfWidth),
		P2:        PerpendicularPoint(p, normalizeBearing(perpBearing+180.0), halfWidth),
		Bearing:   bearing,
	}
}

// CheckCrossing tests whether the GPS segment p0→p1 (timestamps t0, t1 in
// nanoseconds) crosses this TripLine. Returns a Crossing with an interpolated
// timestamp, or nil if there is no intersection.
func (tl *TripLine) CheckCrossing(p0, p1 Point, t0, t1 int64) *Crossing {
	t, _, ok := LineIntersection(p0, p1, tl.P1, tl.P2)
	if !ok {
		return nil
	}
	return &Crossing{
		TripLine: tl,
		Time:     t0 + int64(float64(t1-t0)*t),
		// VehicleID filled by the caller
	}
}

// ComputeSpeed derives a SpeedSample from an entry crossing c1 and an exit
// crossing c2 on the same segment. Returns nil when the inputs are invalid or
// the implied speed is outside a realistic range.
func ComputeSpeed(c1, c2 *Crossing) *SpeedSample {
	if c1.TripLine.SegmentID != c2.TripLine.SegmentID {
		return nil
	}
	if c1.TripLine.Index >= c2.TripLine.Index {
		return nil // must be entry (1) before exit (2)
	}
	dist := c2.TripLine.Dist - c1.TripLine.Dist
	if dist <= 0 {
		return nil
	}
	dtSec := float64(c2.Time-c1.Time) / 1e9
	if dtSec <= 0 {
		return nil
	}
	speed := dist / dtSec
	if speed <= 0 || speed > MaxRealisticSpeedMS {
		return nil
	}
	return &SpeedSample{
		SegmentID: c1.TripLine.SegmentID,
		Time:      c1.Time,
		Speed:     speed,
	}
}

// ─── Geographic helpers ────────────────────────────────────────────────────────

// LineIntersection computes the parametric intersection of two line segments
// p1→p2 and p3→p4. Returns (t, u, ok); ok is true only when both parameters
// are in [0, 1] (i.e. the segments actually cross).
func LineIntersection(p1, p2, p3, p4 Point) (t, u float64, ok bool) {
	d := (p4.Lon-p3.Lon)*(p1.Lat-p2.Lat) - (p1.Lon-p2.Lon)*(p4.Lat-p3.Lat)
	if d == 0 {
		return 0, 0, false
	}
	t = ((p3.Lat-p4.Lat)*(p1.Lon-p3.Lon) + (p3.Lon-p4.Lon)*(p3.Lat-p1.Lat)) / d
	u = ((p1.Lat-p2.Lat)*(p1.Lon-p3.Lon) + (p1.Lon-p2.Lon)*(p3.Lat-p1.Lat)) / d
	return t, u, t >= 0 && t <= 1 && u >= 0 && u <= 1
}

// DistanceMeters returns the Haversine great-circle distance between two points.
func DistanceMeters(p1, p2 Point) float64 {
	dLat := toRad(p2.Lat - p1.Lat)
	dLon := toRad(p2.Lon - p1.Lon)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(p1.Lat))*math.Cos(toRad(p2.Lat))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// Bearing returns the initial bearing (0–360°) from p1 to p2.
func Bearing(p1, p2 Point) float64 {
	dLon := toRad(p2.Lon - p1.Lon)
	lat1, lat2 := toRad(p1.Lat), toRad(p2.Lat)
	y := math.Sin(dLon) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dLon)
	return normalizeBearing(math.Atan2(y, x) * 180.0 / math.Pi)
}

// PerpendicularPoint returns the point reached by travelling distanceMeters
// from p at the given bearing.
func PerpendicularPoint(p Point, bearingDeg, distanceMeters float64) Point {
	lat1 := toRad(p.Lat)
	lon1 := toRad(p.Lon)
	b := toRad(bearingDeg)
	d := distanceMeters / earthRadiusMeters

	lat2 := math.Asin(math.Sin(lat1)*math.Cos(d) +
		math.Cos(lat1)*math.Sin(d)*math.Cos(b))
	lon2 := lon1 + math.Atan2(
		math.Sin(b)*math.Sin(d)*math.Cos(lat1),
		math.Cos(d)-math.Sin(lat1)*math.Sin(lat2),
	)
	return Point{Lat: toDeg(lat2), Lon: toDeg(lon2)}
}

func toRad(deg float64) float64 { return deg * math.Pi / 180.0 }
func toDeg(rad float64) float64 { return rad * 180.0 / math.Pi }

func normalizeBearing(b float64) float64 {
	b = math.Mod(b, 360.0)
	if b < 0 {
		b += 360.0
	}
	return b
}
