package sirtebasin

import (
	"context"
	"fmt"
	"time"

	"hypnotz/internal/types"
)

type ClickHouseClient struct {
	// ClickHouse connection would go here
	// For now, this is a stub that will be filled when ClickHouse is available
}

func NewClickHouseClient(host string) (*ClickHouseClient, error) {
	return &ClickHouseClient{}, nil
}

func (c *ClickHouseClient) QueryClickHouse(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	if req.VehicleID == "" {
		return c.queryAllVehicles(ctx, req)
	}
	return c.queryVehicle(ctx, req)
}

func (c *ClickHouseClient) queryVehicle(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	query := `
		SELECT 
			vehicle_id,
			timestamp,
			lat,
			lon,
			speed,
			heading,
			ingestion_sequence_id
		FROM gps_events
		WHERE vehicle_id = ?
		  AND timestamp >= ?
		  AND timestamp <= ?
		ORDER BY timestamp ASC
	`
	_ = query

	states := make([]types.VehicleState, 0)
	return states, nil
}

func (c *ClickHouseClient) queryAllVehicles(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	query := `
		SELECT 
			vehicle_id,
			timestamp,
			lat,
			lon,
			speed,
			heading,
			ingestion_sequence_id
		FROM gps_events
		WHERE timestamp >= ?
		  AND timestamp <= ?
		ORDER BY vehicle_id, timestamp ASC
	`
	_ = query

	states := make([]types.VehicleState, 0)
	return states, nil
}

func (c *ClickHouseClient) buildQuery(req types.QueryRequest, timeRange types.TimeRange) string {
	baseQuery := "SELECT vehicle_id, timestamp, lat, lon, speed, heading FROM gps_events"

	if req.VehicleID != "" {
		baseQuery += fmt.Sprintf(" WHERE vehicle_id = '%s'", req.VehicleID)
	}

	if !timeRange.Start.IsZero() && !timeRange.End.IsZero() {
		baseQuery += fmt.Sprintf(
			" AND timestamp BETWEEN %d AND %d",
			timeRange.Start.Unix(),
			timeRange.End.Unix(),
		)
	}

	baseQuery += " ORDER BY timestamp ASC"

	return baseQuery
}

func (c *ClickHouseClient) IsStable(state types.VehicleState, threshold time.Duration) bool {
	now := time.Now()
	age := now.Sub(state.Timestamp)
	return age > threshold
}
