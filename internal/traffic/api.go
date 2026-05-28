package traffic

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"hypnotz/internal/memory"
)

// API exposes traffic modeling data over HTTP
type API struct {
	memStore       *memory.MemoryStore
	persistor      *Persistor
	spatialIndex   *SpatialIndex
	segmentIndex   map[int64][]string // segmentID -> []vehicleID
	indexMu        sync.RWMutex
	anomalySubscribers []chan AnomalyReport
	subMu            sync.RWMutex
}

func NewTrafficAPI(memStore *memory.MemoryStore) *API {
	api := &API{
		memStore:    memStore,
		spatialIndex: NewSpatialIndex(9), // Resolution 9 (~0.1 km² hexagons)
		segmentIndex: make(map[int64][]string),
		anomalySubscribers: make([]chan AnomalyReport, 0),
	}
	
	// Start background index builders
	go api.buildSegmentIndex()
	go api.buildSpatialIndex()
	
	return api
}

// SetPersistor attaches the SQLite persistence layer
func (api *API) SetPersistor(p *Persistor) {
	api.persistor = p
}

// buildSegmentIndex periodically rebuilds the segment->vehicles index
func (api *API) buildSegmentIndex() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		api.indexMu.Lock()
		api.segmentIndex = make(map[int64][]string)
		
		entities := api.memStore.GetAll()
		for _, e := range entities {
			if e.LastSpeedSample != nil {
				segID := e.LastSpeedSample.SegmentID
				api.segmentIndex[segID] = append(api.segmentIndex[segID], e.ID)
				// Also update spatial index
				api.spatialIndex.AddSegment(segID, e.Position.Lat, e.Position.Lon)
				api.spatialIndex.AddVehicle(e.ID, e.Position.Lat, e.Position.Lon)
			}
		}
		api.indexMu.Unlock()
	}
}

// buildSpatialIndex periodically updates the H3 spatial index
func (api *API) buildSpatialIndex() {
	// Already integrated into buildSegmentIndex for simplicity
	// Could be separated for finer control
}

// SubscribeToAnomalies returns a channel that receives real-time anomaly reports
func (api *API) SubscribeToAnomalies() chan AnomalyReport {
	ch := make(chan AnomalyReport, 100)
	api.subMu.Lock()
	api.anomalySubscribers = append(api.anomalySubscribers, ch)
	api.subMu.Unlock()
	return ch
}

// NotifyAnomaly broadcasts an anomaly to all SSE subscribers
func (api *API) NotifyAnomaly(anomaly AnomalyReport) {
	api.subMu.RLock()
	for _, ch := range api.anomalySubscribers {
		select {
		case ch <- anomaly:
		default:
			// Channel full, drop message
		}
	}
	api.subMu.RUnlock()
	
	// Persist if available
	if api.persistor != nil {
		api.persistor.SaveAnomaly(
			anomaly.VehicleID,
			anomaly.SegmentID,
			anomaly.Speed,
			anomaly.ExpectedSpeed,
			anomaly.Deviation,
			anomaly.RiskScore,
			anomaly.Timestamp,
		)
	}
}

// SpeedProfileResponse represents the histogram data for a segment
type SpeedProfileResponse struct {
	SegmentID     int64   `json:"segment_id"`
	MeanSpeed     float64 `json:"mean_speed_ms"`
	MeanSpeedKmh  float64 `json:"mean_speed_kmh"`
	SampleCount   int64   `json:"sample_count"`
	StdDev        float64 `json:"std_dev_ms"`
	HourlyAverage map[int]float64 `json:"hourly_average_kmh"` // hour -> speed
	DataPoints    int     `json:"data_points"`
}

// TrafficStateResponse represents current real-time traffic state
type TrafficStateResponse struct {
	Timestamp     int64   `json:"timestamp"`
	ActiveVehicles int    `json:"active_vehicles"`
	Anomalies     []AnomalyReport `json:"anomalies"`
	AverageSpeed  float64 `json:"average_speed_ms"`
	CongestionLevel string `json:"congestion_level"` // LOW, MEDIUM, HIGH
}

// AnomalyReport represents a detected traffic anomaly
type AnomalyReport struct {
	VehicleID   string  `json:"vehicle_id"`
	SegmentID   int64   `json:"segment_id"`
	Speed       float64 `json:"speed_ms"`
	ExpectedSpeed float64 `json:"expected_speed_ms"`
	Deviation   float64 `json:"deviation_std"`
	Timestamp   int64   `json:"timestamp"`
	RiskScore   float64 `json:"risk_score"`
}

// GetSpeedProfile returns the speed histogram for a specific segment
// GET /traffic/segments/{segmentId}/profile
func (api *API) GetSpeedProfile(w http.ResponseWriter, r *http.Request) {
	// Extract segment ID from URL (simplified - in production use router params)
	segmentID := r.URL.Query().Get("segment_id")
	if segmentID == "" {
		http.Error(w, "segment_id required", http.StatusBadRequest)
		return
	}

	// In a real implementation, we'd query a persistent store by segment ID
	// For now, we return aggregated stats from all entities (demo)
	entities := api.memStore.GetAll()
	
	totalSpeed := 0.0
	count := 0
	hourlySums := make(map[int]float64)
	hourlyCounts := make(map[int]int)

	for _, e := range entities {
		if e.SpeedHistogram != nil {
			c, mean, stddev := e.SpeedHistogram.GetStats()
			if c > 0 {
				totalSpeed += mean * float64(c)
				count += int(c)
				
				// Simplified hourly breakdown (would need actual histogram access)
				hour := int(time.Now().Hour())
				hourlySums[hour] += mean * float64(c)
				hourlyCounts[hour] += int(c)
			}
		}
	}

	if count == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"error": "No data available for segment",
		})
		return
	}

	meanSpeed := totalSpeed / float64(count)
	
	hourlyAvg := make(map[int]float64)
	for h, sum := range hourlySums {
		hourlyAvg[h] = (sum / float64(hourlyCounts[h])) * 3.6 // to km/h
	}

	resp := SpeedProfileResponse{
		MeanSpeed:     meanSpeed,
		MeanSpeedKmh:  meanSpeed * 3.6,
		SampleCount:   int64(count),
		StdDev:        0.0, // Would need full histogram merge to compute
		HourlyAverage: hourlyAvg,
		DataPoints:    count,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetCurrentState returns real-time traffic state across all monitored segments
// GET /traffic/state
func (api *API) GetCurrentState(w http.ResponseWriter, r *http.Request) {
	entities := api.memStore.GetAll()
	
	anomalies := []AnomalyReport{}
	totalSpeed := 0.0
	count := 0

	for _, e := range entities {
		if e.LastSpeedSample != nil {
			totalSpeed += e.LastSpeedSample.Speed
			count++
			
			// Check for anomalies
			deviation := e.GetSpeedDeviation()
			if deviation > 2.0 || e.IsAnomalous() {
				anomalies = append(anomalies, AnomalyReport{
					VehicleID:     e.ID,
					Speed:         e.LastSpeedSample.Speed,
					ExpectedSpeed: e.AverageSegmentSpeed,
					Deviation:     deviation,
					Timestamp:     e.LastSpeedSample.Time,
					RiskScore:     e.RiskScore,
				})
			}
		}
	}

	avgSpeed := 0.0
	if count > 0 {
		avgSpeed = totalSpeed / float64(count)
	}

	// Determine congestion level
	level := "LOW"
	if avgSpeed > 25.0 { // >90 km/h
		level = "LOW"
	} else if avgSpeed > 15.0 { // 54-90 km/h
		level = "MEDIUM"
	} else {
		level = "HIGH"
	}

	resp := TrafficStateResponse{
		Timestamp:      time.Now().Unix(),
		ActiveVehicles: len(entities),
		Anomalies:      anomalies,
		AverageSpeed:   avgSpeed,
		CongestionLevel: level,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetSegmentCoverage returns observability metrics (from diagnostics)
// GET /traffic/coverage
func (api *API) GetSegmentCoverage(w http.ResponseWriter, r *http.Request) {
	// This would call the diagnostics.Analyzer
	// For now, return basic counts
	entities := api.memStore.GetAll()
	
	observable := 0
	partial := 0
	dead := 0

	for _, e := range entities {
		if e.SpeedHistogram != nil {
			c, _, _ := e.SpeedHistogram.GetStats()
			if c > 10 {
				observable++
			} else if c > 0 {
				partial++
			} else {
				dead++
			}
		} else {
			dead++
		}
	}

	total := len(entities)
	if total == 0 {
		total = 1 // Avoid division by zero
	}

	resp := map[string]interface{}{
		"total_segments": total,
		"observable_pct": float64(observable) / float64(total),
		"partial_pct":    float64(partial) / float64(total),
		"dead_zone_pct":  float64(dead) / float64(total),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RegisterRoutes registers all traffic API routes on a mux
func (api *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/traffic/segments/profile", api.GetSpeedProfile)
	mux.HandleFunc("/traffic/state", api.GetCurrentState)
	mux.HandleFunc("/traffic/coverage", api.GetSegmentCoverage)
	mux.HandleFunc("/traffic/crossings", api.HandleCrossingInjection)
	mux.HandleFunc("/traffic/stream/anomalies", api.StreamAnomalies)
	
	// H3 Spatial endpoints
	mux.HandleFunc("/traffic/h3/state", api.GetH3TrafficState)
	mux.HandleFunc("/traffic/h3/boundary", api.GetH3Boundary)
	mux.HandleFunc("/traffic/h3/neighbors", api.GetH3Neighbors)
}

// CrossingInjectionRequest represents an external crossing event
type CrossingInjectionRequest struct {
	VehicleID string  `json:"vehicle_id"`
	SegmentID int64   `json:"segment_id"`
	TripLineIndex int `json:"trip_line_index"` // 1=entry, 2=exit
	Distance  float64 `json:"distance_meters"`
	Timestamp int64   `json:"timestamp_ns"`
}

// HandleCrossingInjection accepts external crossing events (POST)
// Useful for hybrid map-matching where another service does the geometric matching
func (api *API) HandleCrossingInjection(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CrossingInjectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Find or create entity
	entity, exists := api.memStore.Get(req.VehicleID)
	if !exists {
		// Create minimal entity for this crossing
		// In production, you'd want more context
		http.Error(w, "Vehicle not found", http.StatusNotFound)
		return
	}

	// Create crossing object
	crossing := &Crossing{
		TripLine: &TripLine{
			SegmentID: req.SegmentID,
			Index:     req.TripLineIndex,
			Dist:      req.Distance,
		},
		Time:      req.Timestamp,
		VehicleID: req.VehicleID,
	}

	// Process through entity's traffic model
	sample := entity.ProcessTrafficCrossing(crossing)

	// If a speed sample was generated, check for anomaly
	if sample != nil {
		deviation := entity.GetSpeedDeviation()
		if deviation > 2.0 || entity.IsAnomalous() {
			anomaly := AnomalyReport{
				VehicleID:     req.VehicleID,
				SegmentID:     req.SegmentID,
				Speed:         sample.Speed,
				ExpectedSpeed: entity.AverageSegmentSpeed,
				Deviation:     deviation,
				Timestamp:     req.Timestamp,
				RiskScore:     entity.RiskScore,
			}
			api.NotifyAnomaly(anomaly)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "accepted",
		"sample_generated": sample != nil,
	})
}

// StreamAnomalies serves Server-Sent Events for real-time anomaly detection
// GET /traffic/stream/anomalies
func (api *API) StreamAnomalies(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := api.SubscribeToAnomalies()
	defer func() {
		// Unsubscribe logic would go here (requires channel tracking)
	}()

	// Send heartbeat every 15s
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case anomaly, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(anomaly)
			w.Write([]byte("data: "))
			w.Write(data)
			w.Write([]byte("\n\n"))
			flusher.Flush()

		case <-heartbeat.C:
			w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// H3TrafficResponse contains traffic state for an H3 hexagon
type H3TrafficResponse struct {
	H3Index        string   `json:"h3_index"`
	Resolution     int      `json:"resolution"`
	SegmentCount   int      `json:"segment_count"`
	VehicleCount   int      `json:"vehicle_count"`
	AverageSpeed   float64  `json:"average_speed_ms"`
	CongestionLevel string  `json:"congestion_level"`
	NeighborStates []string `json:"neighbor_congestion_levels"`
	Boundary       [][2]float64 `json:"boundary"`
}

// GetH3TrafficState returns traffic state for a specific H3 hexagon
// GET /traffic/h3/state?lat=...&lon=...&resolution=9
func (api *API) GetH3TrafficState(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	
	if latStr == "" || lonStr == "" {
		http.Error(w, "lat and lon required", http.StatusBadRequest)
		return
	}
	
	// Parse lat/lon (simplified, use strconv in production)
	lat := -1.2921 // Placeholder
	lon := 36.8219 // Placeholder
	
	hex := api.spatialIndex.GetHexFromLatLng(lat, lon)
	segments := api.spatialIndex.GetSegmentsInHex(hex)
	vehicles := api.spatialIndex.GetVehiclesInHex(hex)
	
	// Calculate average speed from vehicles in hex
	// (Would iterate entities and compute mean)
	
	resp := H3TrafficResponse{
		H3Index:        hex.String(),
		Resolution:     9,
		SegmentCount:   len(segments),
		VehicleCount:   len(vehicles),
		AverageSpeed:   15.0, // Placeholder
		CongestionLevel: "MEDIUM", // Placeholder
		Boundary:       api.spatialIndex.GetHexBoundary(hex),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetH3Boundary returns the hexagon boundary for a given lat/lon
// GET /traffic/h3/boundary?lat=...&lon=...
func (api *API) GetH3Boundary(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	
	if latStr == "" || lonStr == "" {
		http.Error(w, "lat and lon required", http.StatusBadRequest)
		return
	}
	
	lat := -1.2921 // Placeholder
	lon := 36.8219 // Placeholder
	
	hex := api.spatialIndex.GetHexFromLatLng(lat, lon)
	boundary := api.spatialIndex.GetHexBoundary(hex)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"h3_index": hex.String(),
		"boundary": boundary,
	})
}

// GetH3Neighbors returns the 6 adjacent hexagons
// GET /traffic/h3/neighbors?lat=...&lon=...
func (api *API) GetH3Neighbors(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	
	if latStr == "" || lonStr == "" {
		http.Error(w, "lat and lon required", http.StatusBadRequest)
		return
	}
	
	lat := -1.2921 // Placeholder
	lon := 36.8219 // Placeholder
	
	hex := api.spatialIndex.GetHexFromLatLng(lat, lon)
	neighbors := api.spatialIndex.GetNeighbors(hex)
	
	neighborStrs := make([]string, len(neighbors))
	for i, n := range neighbors {
		neighborStrs[i] = n.String()
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"center": hex.String(),
		"neighbors": neighborStrs,
	})
}
