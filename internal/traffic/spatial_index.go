package traffic

import (
	"fmt"
	"sync"

	"github.com/uber/h3-go/v4"
)

// SpatialIndex maps H3 hexagons to the segments and vehicles they contain,
// enabling fast spatial queries without a full table scan.
type SpatialIndex struct {
	mu         sync.RWMutex
	resolution int // H3 resolution, 0–15; 9 ≈ 0.1 km²

	hexToSegments map[h3.Cell][]int64  // hex → segment IDs
	segmentToHex  map[int64]h3.Cell    // segment ID → its hex
	hexToVehicles map[h3.Cell][]string // hex → vehicle IDs
	vehicleToHex  map[string]h3.Cell   // vehicle ID → its current hex
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
// Silently drops the update if the H3 library returns an error for the
// coordinate (e.g. invalid resolution).
func (si *SpatialIndex) AddSegment(segmentID int64, lat, lon float64) {
	cell, err := h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, si.resolution)
	if err != nil {
		return
	}

	si.mu.Lock()
	defer si.mu.Unlock()

	if prev, ok := si.segmentToHex[segmentID]; ok && prev == cell {
		return
	}
	si.segmentToHex[segmentID] = cell
	si.hexToSegments[cell] = append(si.hexToSegments[cell], segmentID)
}

// AddVehicle moves vehicleID to the hexagon covering (lat, lon).
// If the vehicle was in a different hex, it is removed from that hex in O(1).
func (si *SpatialIndex) AddVehicle(vehicleID string, lat, lon float64) {
	cell, err := h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, si.resolution)
	if err != nil {
		return
	}

	si.mu.Lock()
	defer si.mu.Unlock()

	if prev, ok := si.vehicleToHex[vehicleID]; ok {
		if prev == cell {
			return
		}
		si.removeVehicleFromHex(vehicleID, prev)
	}
	si.vehicleToHex[vehicleID] = cell
	si.hexToVehicles[cell] = append(si.hexToVehicles[cell], vehicleID)
}

// RemoveVehicle removes a vehicle from the index entirely.
func (si *SpatialIndex) RemoveVehicle(vehicleID string) {
	si.mu.Lock()
	defer si.mu.Unlock()
	if prev, ok := si.vehicleToHex[vehicleID]; ok {
		si.removeVehicleFromHex(vehicleID, prev)
		delete(si.vehicleToHex, vehicleID)
	}
}

// removeVehicleFromHex is the unlocked inner helper. Caller must hold mu.Lock.
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

// GetSegmentsInHex returns a snapshot of segment IDs in cell.
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

// GetVehiclesInHex returns a snapshot of vehicle IDs in cell.
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
// index's configured resolution. Returns the zero Cell on error.
func (si *SpatialIndex) CellFromLatLng(lat, lon float64) h3.Cell {
	cell, _ := h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, si.resolution)
	return cell
}

// CellFromLatLngE is like CellFromLatLng but surfaces the error, useful when
// the caller needs to distinguish an invalid coordinate from a zero cell.
func (si *SpatialIndex) CellFromLatLngE(lat, lon float64) (h3.Cell, error) {
	cell, err := h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, si.resolution)
	if err != nil {
		return h3.Cell(0), fmt.Errorf("lat/lon (%.6f, %.6f) at res %d: %w", lat, lon, si.resolution, err)
	}
	return cell, nil
}

// Neighbors returns the six cells adjacent to cell (ring-1 disk minus center).
// Returns nil on error.
func (si *SpatialIndex) Neighbors(cell h3.Cell) []h3.Cell {
	disk, err := h3.GridDisk(cell, 1)
	if err != nil {
		return nil
	}
	out := make([]h3.Cell, 0, 6)
	for _, c := range disk {
		if c != cell {
			out = append(out, c)
		}
	}
	return out
}

// CellBoundary returns the boundary polygon of cell as [lat, lon] pairs.
// Returns nil on error.
func (si *SpatialIndex) CellBoundary(cell h3.Cell) [][2]float64 {
	loop, err := h3.CellToBoundary(cell)
	if err != nil {
		return nil
	}
	coords := make([][2]float64, len(loop))
	for i, ll := range loop {
		coords[i] = [2]float64{ll.Lat, ll.Lng}
	}
	return coords
}

// SegmentsInDisk returns all unique segment IDs within k rings of cell.
func (si *SpatialIndex) SegmentsInDisk(cell h3.Cell, k int) []int64 {
	disk, err := h3.GridDisk(cell, k)
	if err != nil {
		return nil
	}

	si.mu.RLock()
	defer si.mu.RUnlock()

	seen := make(map[int64]struct{})
	var out []int64
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
