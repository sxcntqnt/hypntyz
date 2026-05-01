package features

import (
	"math"

	"hypnotz/internal/types"
)

const (
	MaxSpeed      = 120.0
	EarthRadiusKm = 6371.0
	FeatureSize   = 7
)

type Builder struct {
	pool *VectorPool
}

func NewBuilder(pool *VectorPool) *Builder {
	return &Builder{pool: pool}
}

func (b *Builder) Build(v types.Vehicle, focusLat, focusLon float64) types.FeatureVector {
	dx := v.Lat - focusLat
	dy := v.Lon - focusLon
	distance := math.Sqrt(dx*dx + dy*dy)

	anomalyScore := 0.0
	if v.Anomaly {
		anomalyScore = 1.0
	}

	return types.FeatureVector{
		Lat:          v.Lat,
		Lon:          v.Lon,
		Velocity:     v.Speed / MaxSpeed,
		SinHeading:   math.Sin(v.Heading),
		CosHeading:   math.Cos(v.Heading),
		Distance:     distance,
		AnomalyScore: anomalyScore,
		FleetID:      0,
	}
}

func (b *Builder) ToVector(fv types.FeatureVector) []float64 {
	vec := b.pool.Get()
	vec[0] = fv.Lat
	vec[1] = fv.Lon
	vec[2] = fv.Velocity
	vec[3] = fv.SinHeading
	vec[4] = fv.CosHeading
	vec[5] = fv.Distance
	vec[6] = fv.AnomalyScore
	return vec
}

func (b *Builder) BuildQueryVector(client types.ClientState, vehicle types.Vehicle) []float64 {
	vec := b.pool.Get()

	dx := vehicle.Lat - client.FocusLat
	dy := vehicle.Lon - client.FocusLon
	distance := math.Sqrt(dx*dx + dy*dy)

	inViewport := vehicle.Lat >= client.Viewport.MinLat &&
		vehicle.Lat <= client.Viewport.MaxLat &&
		vehicle.Lon >= client.Viewport.MinLon &&
		vehicle.Lon <= client.Viewport.MaxLon

	viewportWeight := 0.0
	if inViewport {
		viewportWeight = 1.0
	}

	anomalyWeight := 0.0
	if client.Preferences.AnomalyPriority && vehicle.Anomaly {
		anomalyWeight = 1.0
	}

	vec[0] = client.FocusLat
	vec[1] = client.FocusLon
	vec[2] = viewportWeight
	vec[3] = anomalyWeight
	vec[4] = distance
	vec[5] = 0.0
	vec[6] = 0.0

	return vec
}

func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(deg float64) float64 {
		return deg * math.Pi / 180.0
	}

	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadiusKm * c
}
