package features

import (
	"math"
	"projection-engine/internal/sirtebasin"
)

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

// Convert raw vehicle → feature vector
func (b *Builder) Build(v sirtebasin.Vehicle, focusLat, focusLon float64) Vector {

	dx := v.Lat - focusLat
	dy := v.Lon - focusLon
	distance := math.Sqrt(dx*dx + dy*dy)

	return Vector{
		v.Lat,
		v.Lon,
		v.Speed / 120.0, // normalize speed
		math.Sin(v.Head),
		math.Cos(v.Head),
		distance,
	}
}
