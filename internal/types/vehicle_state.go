package types

import "time"

// SourceType identifies where a VehicleState reading originated.
type SourceType string

const (
	SourceRedis      SourceType = "redis"
	SourceClickHouse SourceType = "clickhouse"
	SourceMerged     SourceType = "merged"
)

// VehicleState is a single timestamped observation of a vehicle's position
// and kinematics, annotated with provenance metadata.
type VehicleState struct {
	VehicleID   string
	TimestampNS int64
	Timestamp   time.Time
	Lat         float64
	Lon         float64
	Speed       float64
	Heading     float64
	DataSource  SourceType
	Confidence  float64
	AgeMs       int64
	IngestSeq   uint64
}

// QueryMode controls how the Sirtebasin adapter fetches data.
type QueryMode string

const (
	RealtimeOnly    QueryMode = "realtime"
	HistoricalOnly  QueryMode = "historical"
	HybridReconcile QueryMode = "hybrid"
)

// TimeRange is an inclusive half-open interval [Start, End).
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// QueryRequest is issued by the engine to the Sirtebasin adapter each tick.
type QueryRequest struct {
	QueryID   string
	VehicleID string
	TStart    int64
	TEnd      int64
	Mode      QueryMode
	FocusLat  float64
	FocusLon  float64
	TimeRange TimeRange
}

// VehicleStateSet is the response returned by the Sirtebasin adapter.
type VehicleStateSet struct {
	QueryID           string
	TimestampNS       int64
	Vehicles          []VehicleState
	PartialHistorical bool
	StaleRealtime     bool
}

// TensorSequence is the compiled representation fed into the cognitive pipeline.
// Each row in Tokens is a FeatureDim-wide vector corresponding to one time step.
type TensorSequence struct {
	VehicleID    string
	Tokens       [][]float64
	Timestamps   []int64
	Confidence   []float64
	IsTimeSorted bool
}

// FeatureDim is the width of a single token in a TensorSequence.
// Fields: [lat, lon, speed, heading, confidence, time_delta, source].
const FeatureDim = 7

// MaxAgeNS is the staleness threshold beyond which a VehicleState is
// considered too old to include in a live projection.
const MaxAgeNS = int64(5 * time.Minute)
