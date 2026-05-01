package ranker

import (
	"sort"

	"hypnotz/internal/types"
)

type ScoredVehicle struct {
	Vehicle types.Vehicle
	Score   float64
}

type Ranker struct {
	maxResults int
}

func NewRanker(maxResults int) *Ranker {
	return &Ranker{
		maxResults: maxResults,
	}
}

func (r *Ranker) RankAndThin(vehicles []types.Vehicle, scores []float64, maxResults int) []ScoredVehicle {
	if len(vehicles) == 0 || len(scores) == 0 {
		return []ScoredVehicle{}
	}

	scored := make([]ScoredVehicle, len(vehicles))
	for i, v := range vehicles {
		score := 0.0
		if i < len(scores) {
			score = scores[i]
		}
		scored[i] = ScoredVehicle{
			Vehicle: v,
			Score:   score,
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Vehicle.Anomaly && !scored[j].Vehicle.Anomaly {
			return true
		}
		if !scored[i].Vehicle.Anomaly && scored[j].Vehicle.Anomaly {
			return false
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > maxResults {
		scored = scored[:maxResults]
	}

	return scored
}

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

func (r *Ranker) FilterByTypes(vehicles []types.Vehicle, vehicleTypes []string) []types.Vehicle {
	if len(vehicleTypes) == 0 {
		return vehicles
	}

	typeSet := make(map[string]bool)
	for _, t := range vehicleTypes {
		typeSet[t] = true
	}

	filtered := make([]types.Vehicle, 0, len(vehicles))
	for _, v := range vehicles {
		if typeSet[v.Type] {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func (r *Ranker) ToProjections(scored []ScoredVehicle) []types.Projection {
	projections := make([]types.Projection, len(scored))
	for i, sv := range scored {
		projections[i] = types.Projection{
			ID:    sv.Vehicle.ID,
			Lat:   sv.Vehicle.Lat,
			Lon:   sv.Vehicle.Lon,
			Score: sv.Score,
			Speed: sv.Vehicle.Speed,
		}
	}
	return projections
}
