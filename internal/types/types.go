package types

import "time"

// Viewport defines the geographic bounding box a client is observing.
type Viewport struct {
	MinLat float64 `json:"min_lat"`
	MinLon float64 `json:"min_lon"`
	MaxLat float64 `json:"max_lat"`
	MaxLon float64 `json:"max_lon"`
}

// Preferences carries per-client filtering and prioritisation hints.
type Preferences struct {
	VehicleTypes    []string `json:"vehicle_types"`
	AnomalyPriority bool     `json:"anomaly_priority"`
}

// ClientState is the full subscription context for one connected client.
type ClientState struct {
	ID          string      `json:"id"`
	Viewport    Viewport    `json:"viewport"`
	FocusLat    float64     `json:"focus_lat"`
	FocusLon    float64     `json:"focus_lon"`
	Preferences Preferences `json:"preferences"`
	MaxResults  int         `json:"max_results"`
}

// Vehicle is the wire representation of a single tracked vehicle.
type Vehicle struct {
	ID        string    `json:"id"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Speed     float64   `json:"speed"`
	Heading   float64   `json:"heading"`
	Timestamp time.Time `json:"timestamp"`
	FleetID   string    `json:"fleet_id,omitempty"`
	Type      string    `json:"type,omitempty"`
	Anomaly   bool      `json:"anomaly,omitempty"`
}

// Projection is the scored, ranked output that the SSE stream delivers to clients.
type Projection struct {
	ID        string  `json:"id"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Score     float64 `json:"score"`
	Speed     float64 `json:"speed,omitempty"`
	Heading   float64 `json:"heading,omitempty"`
	Timestamp int64   `json:"timestamp"`
}

// ProjectionBatch is the envelope sent to a single client in one tick.
type ProjectionBatch struct {
	ClientID  string       `json:"client_id"`
	Timestamp int64        `json:"timestamp"`
	Vehicles  []Projection `json:"vehicles"`
}

// Vector is a generic floating-point feature slice.
type Vector []float64

// FeatureVector is the fixed-dimension input representation used by the
// attention and ranking stages.
// NOTE: FleetID is hashed to uint32 for embedding purposes; the raw string
// lives on Vehicle.
type FeatureVector struct {
	Lat          float64
	Lon          float64
	Velocity     float64
	SinHeading   float64
	CosHeading   float64
	Distance     float64
	AnomalyScore float64
	FleetIDHash  uint32 // murmur/fnv hash of Vehicle.FleetID
}

// ClientConfig holds tunables for a single engine node.
type ClientConfig struct {
	TickRateHz           int
	MaxVehiclesPerClient int
	MaxClientsPerNode    int
	EnableBackpressure   bool
	RegionID             string
	SirtebasinURL        string // required; empty means sirtebasin is disabled
	RedisURL             string // required; empty means in-process fallback
}

// DefaultClientConfig returns safe production defaults.
// Callers must set SirtebasinURL and RedisURL before use.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		TickRateHz:           20,
		MaxVehiclesPerClient: 500,
		MaxClientsPerNode:    10_000,
		EnableBackpressure:   true,
		RegionID:             "default",
	}
}
