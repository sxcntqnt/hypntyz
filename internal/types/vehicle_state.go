package types

import "time"

type SourceType string

const (
	Redis      SourceType = "redis"
	ClickHouse SourceType = "clickhouse"
	Merged     SourceType = "merged"
)

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

type QueryMode string

const (
	RealtimeOnly    QueryMode = "realtime"
	HistoricalOnly  QueryMode = "historical"
	HybridReconcile QueryMode = "hybrid"
)

type TimeRange struct {
	Start time.Time
	End   time.Time
}

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

type VehicleStateSet struct {
	QueryID           string
	TimestampNS       int64
	Vehicles          []VehicleState
	PartialHistorical bool
	StaleRealtime     bool
}

type TensorSequence struct {
	VehicleID  string
	Tokens     [][]float64
	Timestamps []int64
	Confidence []float64
	IsTimeSorted bool
}

type TemporalSequence struct {
	VehicleID  string
	Tokens     [][]float64
	Timestamps []int64
	Confidence []float64
	IsTimeSorted bool
}

const (
	FeatureDim = 7
	MaxAgeNS   = 300 * int64(time.Second)
)
