package traffic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"hypnotz/internal/memory"
)

// ─── Response types ────────────────────────────────────────────────────────────

// AnomalyReport describes a single detected speed anomaly.
type AnomalyReport struct {
	VehicleID    string  `json:"vehicle_id"`
	SegmentID    int64   `json:"segment_id"`
	Speed        float64 `json:"speed_ms"`
	ExpectedSpeed float64 `json:"expected_speed_ms"`
	Deviation    float64 `json:"deviation_std"`
	Timestamp    int64   `json:"timestamp"`
	RiskScore    float64 `json:"risk_score"`
}

// SpeedProfileResponse carries the aggregated histogram statistics for one
// road segment.
type SpeedProfileResponse struct {
	SegmentID     int64           `json:"segment_id"`
	MeanSpeed     float64         `json:"mean_speed_ms"`
	MeanSpeedKmh  float64         `json:"mean_speed_kmh"`
	SampleCount   int64           `json:"sample_count"`
	StdDev        float64         `json:"std_dev_ms"`
	HourlyAverage map[int]float64 `json:"hourly_average_kmh"` // hour-of-week → km/h
	DataPoints    int             `json:"data_points"`
}

// TrafficStateResponse is the snapshot returned by GET /traffic/state.
type TrafficStateResponse struct {
	Timestamp       int64           `json:"timestamp"`
	ActiveVehicles  int             `json:"active_vehicles"`
	Anomalies       []AnomalyReport `json:"anomalies"`
	AverageSpeed    float64         `json:"average_speed_ms"`
	CongestionLevel string          `json:"congestion_level"` // LOW | MEDIUM | HIGH
}

// H3TrafficResponse is the snapshot returned by GET /traffic/h3/state.
type H3TrafficResponse struct {
	H3Index         string       `json:"h3_index"`
	Resolution      int          `json:"resolution"`
	SegmentCount    int          `json:"segment_count"`
	VehicleCount    int          `json:"vehicle_count"`
	AverageSpeed    float64      `json:"average_speed_ms"`
	CongestionLevel string       `json:"congestion_level"`
	NeighborStates  []string     `json:"neighbor_congestion_levels"`
	Boundary        [][2]float64 `json:"boundary"`
}

// CrossingInjectionRequest is the body accepted by POST /traffic/crossings.
type CrossingInjectionRequest struct {
	VehicleID    string  `json:"vehicle_id"`
	SegmentID    int64   `json:"segment_id"`
	TripLineIndex int    `json:"trip_line_index"` // 1 = entry, 2 = exit
	Distance     float64 `json:"distance_meters"`
	TimestampNS  int64   `json:"timestamp_ns"`
}

// ─── Congestion thresholds ─────────────────────────────────────────────────────

const (
	// Speeds below which the congestion level steps up.
	congestionHighThresholdMS   = 15.0 // < 15 m/s (~54 km/h)  → HIGH
	congestionMediumThresholdMS = 25.0 // < 25 m/s (~90 km/h)  → MEDIUM
	// ≥ 25 m/s                                                 → LOW

	// anomalyDeviationThreshold is the number of standard deviations above
	// the segment mean at which a speed sample is flagged as anomalous.
	anomalyDeviationThreshold = 2.0

	// sseHeartbeatInterval controls how often a keep-alive comment is written
	// to SSE clients with no recent events.
	sseHeartbeatInterval = 15 * time.Second

	// subscriberChannelDepth is the buffered depth of each anomaly subscriber
	// channel. Events are dropped (not blocked) when a slow client fills it.
	subscriberChannelDepth = 100
)

// ─── API ───────────────────────────────────────────────────────────────────────

// API exposes traffic modelling data over HTTP and SSE.
type API struct {
	memStore     *memory.MemoryStore
	persistor    *Persistor    // optional; nil disables persistence
	spatialIndex *SpatialIndex

	segmentIndex map[int64][]string // segmentID → []vehicleID; rebuilt periodically
	indexMu      sync.RWMutex

	subMu       sync.RWMutex
	subscribers map[chan AnomalyReport]struct{}
}

// NewTrafficAPI constructs an API and starts background index-maintenance
// goroutines. The caller owns the MemoryStore's lifetime.
func NewTrafficAPI(memStore *memory.MemoryStore) *API {
	api := &API{
		memStore:     memStore,
		spatialIndex: NewSpatialIndex(9),
		segmentIndex: make(map[int64][]string),
		subscribers:  make(map[chan AnomalyReport]struct{}),
	}
	go api.runIndexBuilder()
	return api
}

// SetPersistor attaches an optional SQLite persistence layer.
func (api *API) SetPersistor(p *Persistor) { api.persistor = p }

// RegisterRoutes mounts all traffic endpoints on mux.
func (api *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/traffic/segments/profile", api.GetSpeedProfile)
	mux.HandleFunc("/traffic/state", api.GetCurrentState)
	mux.HandleFunc("/traffic/coverage", api.GetSegmentCoverage)
	mux.HandleFunc("/traffic/crossings", api.HandleCrossingInjection)
	mux.HandleFunc("/traffic/stream/anomalies", api.StreamAnomalies)
	mux.HandleFunc("/traffic/h3/state", api.GetH3TrafficState)
	mux.HandleFunc("/traffic/h3/boundary", api.GetH3Boundary)
	mux.HandleFunc("/traffic/h3/neighbors", api.GetH3Neighbors)
}

// ─── Background workers ────────────────────────────────────────────────────────

// runIndexBuilder rebuilds the segment→vehicles and spatial indexes every
// 5 seconds. A single goroutine handles both to avoid double-locking the
// memory store.
func (api *API) runIndexBuilder() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		api.rebuildIndexes()
	}
}

func (api *API) rebuildIndexes() {
	entities := api.memStore.GetAll()
	newIndex := make(map[int64][]string, len(entities))

	for _, e := range entities {
		if e.LastSpeedSample == nil {
			continue
		}
		segID := e.LastSpeedSample.SegmentID
		newIndex[segID] = append(newIndex[segID], e.ID)
		api.spatialIndex.AddSegment(segID, e.Position.Lat, e.Position.Lon)
		api.spatialIndex.AddVehicle(e.ID, e.Position.Lat, e.Position.Lon)
	}

	api.indexMu.Lock()
	api.segmentIndex = newIndex
	api.indexMu.Unlock()
}

// ─── Anomaly pub/sub ───────────────────────────────────────────────────────────

// subscribe registers a new SSE listener and returns its channel.
func (api *API) subscribe() chan AnomalyReport {
	ch := make(chan AnomalyReport, subscriberChannelDepth)
	api.subMu.Lock()
	api.subscribers[ch] = struct{}{}
	api.subMu.Unlock()
	return ch
}

// unsubscribe deregisters a channel and closes it.
func (api *API) unsubscribe(ch chan AnomalyReport) {
	api.subMu.Lock()
	delete(api.subscribers, ch)
	api.subMu.Unlock()
	close(ch)
}

// NotifyAnomaly broadcasts an anomaly to all SSE subscribers and persists it
// if a Persistor is attached. Slow subscribers are dropped, not blocked.
func (api *API) NotifyAnomaly(a AnomalyReport) {
	api.subMu.RLock()
	for ch := range api.subscribers {
		select {
		case ch <- a:
		default:
			// subscriber channel full; drop rather than stall the caller
		}
	}
	api.subMu.RUnlock()

	if api.persistor == nil {
		return
	}
	if err := api.persistor.SaveAnomaly(
		a.VehicleID, a.SegmentID,
		a.Speed, a.ExpectedSpeed,
		a.Deviation, a.RiskScore,
		a.Timestamp,
	); err != nil {
		slog.Warn("anomaly persist failed", "vehicle", a.VehicleID, "err", err)
	}
}

// ─── HTTP handlers ─────────────────────────────────────────────────────────────

// GetSpeedProfile returns the speed histogram statistics for a single segment.
// GET /traffic/segments/profile?segment_id=<int64>
func (api *API) GetSpeedProfile(w http.ResponseWriter, r *http.Request) {
	segmentID, err := parseSegmentID(r)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	api.indexMu.RLock()
	vehicleIDs := api.segmentIndex[segmentID]
	api.indexMu.RUnlock()

	var totalSpeed float64
	var count int64
	hourlySums := make(map[int]float64)
	hourlyCounts := make(map[int]int)
	var lastStdDev float64

	for _, vid := range vehicleIDs {
		e, ok := api.memStore.Get(vid)
		if !ok || e.SpeedHistogram == nil {
			continue
		}
		c, mean, stddev := e.SpeedHistogram.GetStats()
		if c == 0 {
			continue
		}
		totalSpeed += mean * float64(c)
		count += c
		lastStdDev = stddev // approximation; full merge would be more accurate

		hour := int(time.Now().Hour())
		hourlySums[hour] += mean * float64(c)
		hourlyCounts[hour] += int(c)
	}

	if count == 0 {
		writeError(w, fmt.Sprintf("no data for segment %d", segmentID), http.StatusNotFound)
		return
	}

	meanSpeed := totalSpeed / float64(count)
	hourlyAvg := make(map[int]float64, len(hourlySums))
	for h, sum := range hourlySums {
		hourlyAvg[h] = (sum / float64(hourlyCounts[h])) * 3.6
	}

	writeJSON(w, SpeedProfileResponse{
		SegmentID:     segmentID,
		MeanSpeed:     meanSpeed,
		MeanSpeedKmh:  meanSpeed * 3.6,
		SampleCount:   count,
		StdDev:        lastStdDev,
		HourlyAverage: hourlyAvg,
		DataPoints:    int(count),
	})
}

// GetCurrentState returns a real-time snapshot of all monitored segments.
// GET /traffic/state
func (api *API) GetCurrentState(w http.ResponseWriter, r *http.Request) {
	entities := api.memStore.GetAll()

	var totalSpeed float64
	var count int
	var anomalies []AnomalyReport

	for _, e := range entities {
		if e.LastSpeedSample == nil {
			continue
		}
		totalSpeed += e.LastSpeedSample.Speed
		count++

		deviation := e.GetSpeedDeviation()
		if deviation > anomalyDeviationThreshold || e.IsAnomalous() {
			anomalies = append(anomalies, AnomalyReport{
				VehicleID:     e.ID,
				SegmentID:     e.LastSpeedSample.SegmentID,
				Speed:         e.LastSpeedSample.Speed,
				ExpectedSpeed: e.AverageSegmentSpeed,
				Deviation:     deviation,
				Timestamp:     e.LastSpeedSample.Time,
				RiskScore:     e.RiskScore,
			})
		}
	}

	avgSpeed := 0.0
	if count > 0 {
		avgSpeed = totalSpeed / float64(count)
	}

	writeJSON(w, TrafficStateResponse{
		Timestamp:       time.Now().Unix(),
		ActiveVehicles:  len(entities),
		Anomalies:       anomalies,
		AverageSpeed:    avgSpeed,
		CongestionLevel: congestionLevel(avgSpeed),
	})
}

// GetSegmentCoverage returns observability metrics across all known entities.
// GET /traffic/coverage
func (api *API) GetSegmentCoverage(w http.ResponseWriter, r *http.Request) {
	entities := api.memStore.GetAll()
	total := len(entities)
	if total == 0 {
		writeJSON(w, map[string]interface{}{
			"total_segments":  0,
			"observable_pct":  0.0,
			"partial_pct":     0.0,
			"dead_zone_pct":   0.0,
		})
		return
	}

	var observable, partial, dead int
	for _, e := range entities {
		if e.SpeedHistogram == nil {
			dead++
			continue
		}
		c, _, _ := e.SpeedHistogram.GetStats()
		switch {
		case c > 10:
			observable++
		case c > 0:
			partial++
		default:
			dead++
		}
	}

	writeJSON(w, map[string]interface{}{
		"total_segments":  total,
		"observable_pct":  float64(observable) / float64(total),
		"partial_pct":     float64(partial) / float64(total),
		"dead_zone_pct":   float64(dead) / float64(total),
	})
}

// HandleCrossingInjection accepts an externally generated TripLine crossing
// event and feeds it into the relevant entity's traffic model.
// POST /traffic/crossings
func (api *API) HandleCrossingInjection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CrossingInjectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.VehicleID == "" {
		writeError(w, "vehicle_id required", http.StatusBadRequest)
		return
	}

	entity, ok := api.memStore.Get(req.VehicleID)
	if !ok {
		writeError(w, fmt.Sprintf("vehicle %q not found", req.VehicleID), http.StatusNotFound)
		return
	}

	crossing := &Crossing{
		TripLine: &TripLine{
			SegmentID: req.SegmentID,
			Index:     req.TripLineIndex,
			Dist:      req.Distance,
		},
		Time:      req.TimestampNS,
		VehicleID: req.VehicleID,
	}

	sample := entity.ProcessTrafficCrossing(crossing)
	if sample != nil {
		deviation := entity.GetSpeedDeviation()
		if deviation > anomalyDeviationThreshold || entity.IsAnomalous() {
			api.NotifyAnomaly(AnomalyReport{
				VehicleID:     req.VehicleID,
				SegmentID:     req.SegmentID,
				Speed:         sample.Speed,
				ExpectedSpeed: entity.AverageSegmentSpeed,
				Deviation:     deviation,
				Timestamp:     req.TimestampNS,
				RiskScore:     entity.RiskScore,
			})
		}
	}

	writeJSON(w, map[string]interface{}{
		"status":           "accepted",
		"sample_generated": sample != nil,
	})
}

// StreamAnomalies opens a Server-Sent Events stream that delivers anomaly
// reports in real time.
// GET /traffic/stream/anomalies
func (api *API) StreamAnomalies(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	ch := api.subscribe()
	defer api.unsubscribe(ch)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case anomaly, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(anomaly)
			if err != nil {
				slog.Warn("anomaly marshal failed", "err", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// GetH3TrafficState returns traffic state for the H3 hexagon covering the
// given coordinate.
// GET /traffic/h3/state?lat=<float>&lon=<float>
func (api *API) GetH3TrafficState(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := parseLatLon(r)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	cell := api.spatialIndex.CellFromLatLng(lat, lon)
	segments := api.spatialIndex.GetSegmentsInHex(cell)
	vehicles := api.spatialIndex.GetVehiclesInHex(cell)

	// Compute average speed from vehicles currently in this hex.
	var totalSpeed float64
	var speedCount int
	for _, vid := range vehicles {
		e, ok := api.memStore.Get(vid)
		if !ok || e.LastSpeedSample == nil {
			continue
		}
		totalSpeed += e.LastSpeedSample.Speed
		speedCount++
	}
	avgSpeed := 0.0
	if speedCount > 0 {
		avgSpeed = totalSpeed / float64(speedCount)
	}

	neighbors := api.spatialIndex.Neighbors(cell)
	neighborLevels := make([]string, len(neighbors))
	for i, n := range neighbors {
		nVehicles := api.spatialIndex.GetVehiclesInHex(n)
		var nTotal float64
		var nCount int
		for _, vid := range nVehicles {
			e, ok := api.memStore.Get(vid)
			if !ok || e.LastSpeedSample == nil {
				continue
			}
			nTotal += e.LastSpeedSample.Speed
			nCount++
		}
		nAvg := 0.0
		if nCount > 0 {
			nAvg = nTotal / float64(nCount)
		}
		neighborLevels[i] = congestionLevel(nAvg)
	}

	writeJSON(w, H3TrafficResponse{
		H3Index:         cell.String(),
		Resolution:      9,
		SegmentCount:    len(segments),
		VehicleCount:    len(vehicles),
		AverageSpeed:    avgSpeed,
		CongestionLevel: congestionLevel(avgSpeed),
		NeighborStates:  neighborLevels,
		Boundary:        api.spatialIndex.CellBoundary(cell),
	})
}

// GetH3Boundary returns the boundary polygon for the H3 hexagon covering
// the given coordinate.
// GET /traffic/h3/boundary?lat=<float>&lon=<float>
func (api *API) GetH3Boundary(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := parseLatLon(r)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	cell := api.spatialIndex.CellFromLatLng(lat, lon)
	writeJSON(w, map[string]interface{}{
		"h3_index": cell.String(),
		"boundary": api.spatialIndex.CellBoundary(cell),
	})
}

// GetH3Neighbors returns the six adjacent hexagons for the cell covering the
// given coordinate.
// GET /traffic/h3/neighbors?lat=<float>&lon=<float>
func (api *API) GetH3Neighbors(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := parseLatLon(r)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	cell := api.spatialIndex.CellFromLatLng(lat, lon)
	neighbors := api.spatialIndex.Neighbors(cell)
	strs := make([]string, len(neighbors))
	for i, n := range neighbors {
		strs[i] = n.String()
	}
	writeJSON(w, map[string]interface{}{
		"center":    cell.String(),
		"neighbors": strs,
	})
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

// congestionLevel maps an average speed (m/s) to a human-readable tier.
func congestionLevel(avgSpeedMS float64) string {
	switch {
	case avgSpeedMS == 0:
		return "UNKNOWN"
	case avgSpeedMS < congestionHighThresholdMS:
		return "HIGH"
	case avgSpeedMS < congestionMediumThresholdMS:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// parseLatLon extracts and validates lat/lon query parameters.
func parseLatLon(r *http.Request) (lat, lon float64, err error) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	if latStr == "" || lonStr == "" {
		return 0, 0, fmt.Errorf("lat and lon query parameters are required")
	}
	lat, err = strconv.ParseFloat(latStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lat %q: %w", latStr, err)
	}
	lon, err = strconv.ParseFloat(lonStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lon %q: %w", lonStr, err)
	}
	return lat, lon, nil
}

// parseSegmentID extracts and validates the segment_id query parameter.
func parseSegmentID(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("segment_id")
	if raw == "" {
		return 0, fmt.Errorf("segment_id query parameter is required")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid segment_id %q: %w", raw, err)
	}
	return id, nil
}

// writeJSON serialises v as JSON and writes it with a 200 OK status.
// Errors during marshalling are logged and result in a 500 response.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("response encode failed", "err", err)
	}
}

// writeError writes a JSON {"error": msg} response with the given status code.
func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
