package ranker

import (
	"sort"

	"hypnotz/internal/types"
)

// ScoredVehicle pairs a Vehicle with its computed attention score.
type ScoredVehicle struct {
	Vehicle types.Vehicle
	Score   float64
}

// Ranker sorts and thins a set of scored vehicles down to a configured top-K,
// with anomalous vehicles always promoted to the front regardless of score.
type Ranker struct {
	maxResults int
}

// NewRanker constructs a Ranker that caps output at maxResults vehicles.
func NewRanker(maxResults int) *Ranker {
	return &Ranker{maxResults: maxResults}
}

// RankAndThin sorts vehicles by (anomaly flag, score desc) and returns the
// top maxResults entries. Anomalous vehicles are always promoted ahead of
// non-anomalous ones irrespective of their numeric score.
func (r *Ranker) RankAndThin(vehicles []types.Vehicle, scores []float64, maxResults int) []ScoredVehicle {
	if len(vehicles) == 0 {
		return []ScoredVehicle{}
	}

	scored := make([]ScoredVehicle, len(vehicles))
	for i, v := range vehicles {
		score := 0.0
		if i < len(scores) {
			score = scores[i]
		}
		scored[i] = ScoredVehicle{Vehicle: v, Score: score}
	}

	sort.Slice(scored, func(i, j int) bool {
		// Anomalous vehicles surface first.
		if scored[i].Vehicle.Anomaly != scored[j].Vehicle.Anomaly {
			return scored[i].Vehicle.Anomaly
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > maxResults {
		scored = scored[:maxResults]
	}
	return scored
}

// RankProjections sorts projections by score descending and returns the top
// r.maxResults entries. Unlike RankAndThin it works directly on Projection
// slices, useful when the caller has already scored and assembled projections.
func (r *Ranker) RankProjections(projections []types.Projection) []types.Projection {
	if len(projections) == 0 {
		return projections
	}
	sort.Slice(projections, func(i, j int) bool {
		return projections[i].Score > projections[j].Score
	})
	if len(projections) > r.maxResults {
		projections = projections[:r.maxResults]
	}
	return projections
}

// ToProjections converts a ranked ScoredVehicle slice into the Projection
// wire format sent to clients over SSE.
func (r *Ranker) ToProjections(scored []ScoredVehicle) []types.Projection {
	projections := make([]types.Projection, len(scored))
	for i, sv := range scored {
		projections[i] = types.Projection{
			ID:        sv.Vehicle.ID,
			Lat:       sv.Vehicle.Lat,
			Lon:       sv.Vehicle.Lon,
			Score:     sv.Score,
			Speed:     sv.Vehicle.Speed,
			Heading:   sv.Vehicle.Heading,
			Timestamp: sv.Vehicle.Timestamp.UnixNano(),
		}
	}
	return projections
}

// FilterByViewport returns only vehicles whose position falls within viewport.
func (r *Ranker) FilterByViewport(vehicles []types.Vehicle, viewport types.Viewport) []types.Vehicle {
	filtered := make([]types.Vehicle, 0, len(vehicles))
	for _, v := range vehicles {
		if v.Lat >= viewport.MinLat && v.Lat <= viewport.MaxLat &&
			v.Lon >= viewport.MinLon && v.Lon <= viewport.MaxLon {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// FilterByTypes returns only vehicles whose Type appears in vehicleTypes.
// Returns all vehicles unchanged when vehicleTypes is empty.
func (r *Ranker) FilterByTypes(vehicles []types.Vehicle, vehicleTypes []string) []types.Vehicle {
	if len(vehicleTypes) == 0 {
		return vehicles
	}
	typeSet := make(map[string]struct{}, len(vehicleTypes))
	for _, t := range vehicleTypes {
		typeSet[t] = struct{}{}
	}
	filtered := make([]types.Vehicle, 0, len(vehicles))
	for _, v := range vehicles {
		if _, ok := typeSet[v.Type]; ok {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
