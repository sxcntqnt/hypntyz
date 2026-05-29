package traffic

import (
	"sync"

	"github.com/uber/h3-go/v4"
)

// SpatialIndex maps H3 hexagons to the segments and vehicles they contain,
// enabling fast spatial queries without a full table scan.
type SpatialIndex struct {
	mu sync.RWMutex

	resolution int // H3 resolution, 0–15; 9 ≈ 0.1 km²

	hexToSegments map[h3.Cell][]int64  // hex → segment IDs contained within
	segmentToHex  map[int64]h3.Cell    // reverse: segment ID → its hex
	hexToVehicles map[h3.Cell][]string // hex → vehicle IDs currently inside
	vehicleToHex  map[string]h3.Cell   // reverse: vehicle ID → its current hex
}

// NewSpatialIndex creates an H3-based spatial index at the given resolution.
// Resolution 9 (~0.1 km²) is recommended for urban traffic monitoring.
func NewSpatialIndex(resolution int) *SpatialIndex {
	return &SpatialIndex{
		resolution:    resolution,
		hexToSegments: make(map[h3.Cell][]int64),
		segmentToHex:  make(map[int64]h3.Cell),
		hexToVehicles: make(map[h3.Cell][]string),
		vehicleToHex:  make(map[string]h3.Cell),
	}
}

// AddSegment indexes segmentID at the hexagon covering (lat, lon).
// Calling AddSegment again for the same ID is a no-op if the hex is unchanged.
func (si *SpatialIndex) AddSegment(segmentID int64, lat, lon float64) {
	cell := h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, si.resolution)

	si.mu.Lock()
	defer si.mu.Unlock()

	if prev, ok := si.segmentToHex[segmentID]; ok && prev == cell {
		return // nothing to do
	}
	si.segmentToHex[segmentID] = cell
	si.hexToSegments[cell] = append(si.hexToSegments[cell], segmentID)
}

// AddVehicle moves vehicleID to the hexagon covering (lat, lon).
// If the vehicle was previously indexed in a different hex, it is removed
// from that hex in O(1) via the reverse map.
func (si *SpatialIndex) AddVehicle(vehicleID string, lat, lon float64) {
	cell := h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, si.resolution)

	si.mu.Lock()
	defer si.mu.Unlock()

	if prev, ok := si.vehicleToHex[vehicleID]; ok {
		if prev == cell {
			return // already in the right hex
		}
		si.removeVehicleFromHex(vehicleID, prev)
	}
	si.vehicleToHex[vehicleID] = cell
	si.hexToVehicles[cell] = append(si.hexToVehicles[cell], vehicleID)
}

// RemoveVehicle removes a vehicle from the spatial index entirely.
// Safe to call even if the vehicle was never indexed.
func (si *SpatialIndex) RemoveVehicle(vehicleID string) {
	si.mu.Lock()
	defer si.mu.Unlock()
	if prev, ok := si.vehicleToHex[vehicleID]; ok {
		si.removeVehicleFromHex(vehicleID, prev)
		delete(si.vehicleToHex, vehicleID)
	}
}

// removeVehicleFromHex is the unlocked inner helper — caller must hold mu.Lock.
func (si *SpatialIndex) removeVehicleFromHex(vehicleID string, cell h3.Cell) {
	list := si.hexToVehicles[cell]
	for i, v := range list {
		if v == vehicleID {
			last := len(list) - 1
			list[i] = list[last]
			si.hexToVehicles[cell] = list[:last]
			return
		}
	}
}

// GetSegmentsInHex returns a snapshot of all segment IDs in cell.
func (si *SpatialIndex) GetSegmentsInHex(cell h3.Cell) []int64 {
	si.mu.RLock()
	defer si.mu.RUnlock()
	src := si.hexToSegments[cell]
	if len(src) == 0 {
		return nil
	}
	out := make([]int64, len(src))
	copy(out, src)
	return out
}

// GetVehiclesInHex returns a snapshot of all vehicle IDs in cell.
func (si *SpatialIndex) GetVehiclesInHex(cell h3.Cell) []string {
	si.mu.RLock()
	defer si.mu.RUnlock()
	src := si.hexToVehicles[cell]
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// CellFromLatLng converts a geographic coordinate to the H3 cell at the
// index's configured resolution.
func (si *SpatialIndex) CellFromLatLng(lat, lon float64) h3.Cell {
	return h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, si.resolution)
}

// Neighbors returns the six cells adjacent to cell (the ring-1 disk minus
// the center). The returned slice always has exactly 6 elements for valid
// H3 cells at resolutions 1–15.
func (si *SpatialIndex) Neighbors(cell h3.Cell) []h3.Cell {
	disk := h3.GridDisk(cell, 1) // center + 6 neighbours = 7
	out := make([]h3.Cell, 0, 6)
	for _, c := range disk {
		if c != cell {
			out = append(out, c)
		}
	}
	return out
}

// CellBoundary returns the boundary polygon of cell as a sequence of
// [lat, lon] pairs suitable for map rendering.
func (si *SpatialIndex) CellBoundary(cell h3.Cell) [][2]float64 {
	loop := h3.CellToBoundary(cell)
	coords := make([][2]float64, len(loop))
	for i, ll := range loop {
		coords[i] = [2]float64{ll.Lat, ll.Lng}
	}
	return coords
}

// SegmentsInDisk returns all segment IDs within k rings of cell (inclusive).
// Useful for fetching context around a point of interest.
func (si *SpatialIndex) SegmentsInDisk(cell h3.Cell, k int) []int64 {
	disk := h3.GridDisk(cell, k)
	si.mu.RLock()
	defer si.mu.RUnlock()
	var out []int64
	seen := make(map[int64]struct{})
	for _, c := range disk {
		for _, seg := range si.hexToSegments[c] {
			if _, dup := seen[seg]; !dup {
				seen[seg] = struct{}{}
				out = append(out, seg)
			}
		}
	}
	return out
}
