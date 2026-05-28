package traffic

import (
	"sync"

	"github.com/uber/h3-go/v4"
)

// SpatialIndex maps H3 hexagons to traffic data
type SpatialIndex struct {
	mu           sync.RWMutex
	resolution   int // H3 resolution (0-15)
	hexToSegments map[h3.Index][]int64      // H3 index -> segment IDs
	segmentToHex  map[int64]h3.Index        // Segment ID -> H3 index
	hexToVehicles map[h3.Index][]string     // H3 index -> vehicle IDs
}

// NewSpatialIndex creates an H3-based spatial index
func NewSpatialIndex(resolution int) *SpatialIndex {
	return &SpatialIndex{
		resolution:    resolution,
		hexToSegments: make(map[h3.Index][]int64),
		segmentToHex:  make(map[int64]h3.Index),
		hexToVehicles: make(map[h3.Index][]string),
	}
}

// AddSegment indexes a segment by its centroid's H3 index
func (si *SpatialIndex) AddSegment(segmentID int64, lat, lon float64) {
	si.mu.Lock()
	defer si.mu.Unlock()

	h := h3.FromGeo(h3.GeoCoord{Lat: lat, Lng: lon}, si.resolution)
	si.segmentToHex[segmentID] = h
	si.hexToSegments[h] = append(si.hexToSegments[h], segmentID)
}

// AddVehicle indexes a vehicle by its current position's H3 index
func (si *SpatialIndex) AddVehicle(vehicleID string, lat, lon float64) {
	si.mu.Lock()
	defer si.mu.Unlock()

	h := h3.FromGeo(h3.GeoCoord{Lat: lat, Lng: lon}, si.resolution)
	
	// Remove from old hex if exists (simple approach, could optimize)
	for hex, vehicles := range si.hexToVehicles {
		for i, v := range vehicles {
			if v == vehicleID {
				si.hexToVehicles[hex] = append(vehicles[:i], vehicles[i+1:]...)
				break
			}
		}
	}
	
	si.hexToVehicles[h] = append(si.hexToVehicles[h], vehicleID)
}

// GetSegmentsInHex returns all segments in a given H3 hexagon
func (si *SpatialIndex) GetSegmentsInHex(hex h3.Index) []int64 {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.hexToSegments[hex]
}

// GetVehiclesInHex returns all vehicles in a given H3 hexagon
func (si *SpatialIndex) GetVehiclesInHex(hex h3.Index) []string {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.hexToVehicles[hex]
}

// GetTrafficInArea returns aggregated traffic state for an H3 hexagon and its neighbors
func (si *SpatialIndex) GetTrafficInArea(hex h3.Index, api *API) TrafficStateResponse {
	// Get center hex + 1-ring neighbors
	neighbors := h3.GridDisk(hex, 1) // Returns center + 6 neighbors = 7 hexes
	
	allSegments := []int64{}
	for _, h := range neighbors {
		allSegments = append(allSegments, si.GetSegmentsInHex(h)...)
	}
	
	// Aggregate traffic state for these segments
	// (This would query the persistor or in-memory histograms)
	
	return TrafficStateResponse{
		// Populated based on segment data
	}
}

// GetHexFromLatLng converts lat/lon to H3 index
func (si *SpatialIndex) GetHexFromLatLng(lat, lon float64) h3.Index {
	return h3.FromGeo(h3.GeoCoord{Lat: lat, Lng: lon}, si.resolution)
}

// GetNeighbors returns the 6 adjacent hexagons
func (si *SpatialIndex) GetNeighbors(hex h3.Index) []h3.Index {
	return h3.GridDisk(hex, 1)[1:] // Skip center (index 0)
}

// GetHexBoundary returns the hexagon boundary as a list of lat/lon
func (si *SpatialIndex) GetHexBoundary(hex h3.Index) [][2]float64 {
	boundary := h3.ToGeoBoundary(hex)
	coords := make([][2]float64, len(boundary))
	for i, c := range boundary {
		coords[i] = [2]float64{c.Lat, c.Lng}
	}
	return coords
}