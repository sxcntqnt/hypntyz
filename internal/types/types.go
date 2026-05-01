package types

import "time"

type Viewport struct {
	MinLat float64 `json:"min_lat"`
	MinLon float64 `json:"min_lon"`
	MaxLat float64 `json:"max_lat"`
	MaxLon float64 `json:"max_lon"`
}

type Preferences struct {
	VehicleTypes    []string `json:"vehicle_types"`
	AnomalyPriority bool     `json:"anomaly_priority"`
}

type ClientState struct {
	ID          string      `json:"id"`
	Viewport    Viewport    `json:"viewport"`
	FocusLat    float64     `json:"focus_lat"`
	FocusLon    float64     `json:"focus_lon"`
	Preferences Preferences `json:"preferences"`
	MaxResults  int         `json:"max_results"`
}

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

type Projection struct {
	ID        string  `json:"id"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Score     float64 `json:"score"`
	Speed     float64 `json:"speed,omitempty"`
	Heading   float64 `json:"heading,omitempty"`
	Timestamp int64   `json:"timestamp"`
}

type ProjectionBatch struct {
	ClientID  string       `json:"client_id"`
	Timestamp int64        `json:"timestamp"`
	Vehicles  []Projection `json:"vehicles"`
}

type Vector []float64

type FeatureVector struct {
	Lat          float64
	Lon          float64
	Velocity     float64
	SinHeading   float64
	CosHeading   float64
	Distance     float64
	AnomalyScore float64
	FleetID      uint32
}

type ClientConfig struct {
	TickRateHz           int
	MaxVehiclesPerClient int
	MaxClientsPerNode    int
	EnableBackpressure   bool
	RegionID             string
	SirtebasinURL        string
	RedisURL             string
}

func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		TickRateHz:           20,
		MaxVehiclesPerClient: 500,
		MaxClientsPerNode:    10000,
		EnableBackpressure:   true,
		RegionID:             "default",
	}
}
