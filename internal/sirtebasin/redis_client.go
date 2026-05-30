package sirtebasin

import (
	"context"
	"fmt"
	"time"

	"hypnotz/internal/types"
)

// RedisClient queries the Redis stream for real-time vehicle state.
// The connection field is intentionally left as a stub; wire in a real client
// (e.g. github.com/redis/go-redis/v9) when Redis is available.
type RedisClient struct {
	// conn *redis.Client  // TODO: inject real client
}

// NewRedisClient constructs a RedisClient pointed at url.
func NewRedisClient(url string) (*RedisClient, error) {
	// TODO: open real connection, ping, return error on failure.
	return &RedisClient{}, nil
}

// QueryRedis dispatches to a single-vehicle or all-vehicles query depending on
// whether req.VehicleID is set.
func (r *RedisClient) QueryRedis(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	if req.VehicleID == "" {
		return r.queryAllVehicles(ctx, req)
	}
	return r.queryVehicle(ctx, req)
}

// queryVehicle fetches the state history for a single vehicle within the
// request's time range, defaulting to the last 5 minutes when unset.
func (r *RedisClient) queryVehicle(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	key := fmt.Sprintf("vehicle:%s", req.VehicleID)
	_ = key // TODO: XRANGE key from req.TimeRange.Start to req.TimeRange.End

	now := time.Now()
	if req.TimeRange.Start.IsZero() {
		req.TimeRange.Start = now.Add(-5 * time.Minute)
	}
	if req.TimeRange.End.IsZero() {
		req.TimeRange.End = now
	}

	return []types.VehicleState{}, nil
}

// queryAllVehicles scans all tracked vehicles and returns their states within
// the request's time range.
func (r *RedisClient) queryAllVehicles(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	// TODO: SCAN vehicle:* and XRANGE each key.
	return []types.VehicleState{}, nil
}

// GetLatestState returns the most recent state for vehicleID, or nil when the
// vehicle is not in Redis.
func (r *RedisClient) GetLatestState(ctx context.Context, vehicleID string) (*types.VehicleState, error) {
	key := fmt.Sprintf("vehicle:%s", vehicleID)
	_ = key // TODO: XREVRANGE key COUNT 1
	return nil, nil
}

// GetStatesInWindow returns all states for vehicleID between start and end.
func (r *RedisClient) GetStatesInWindow(ctx context.Context, vehicleID string, start, end time.Time) ([]types.VehicleState, error) {
	// TODO: XRANGE vehicle:<id> start end
	return []types.VehicleState{}, nil
}

// confidenceFor returns the confidence score for state at now.
// Delegates to the package-level ComputeConfidence so the logic stays in one place.
func (r *RedisClient) confidenceFor(state types.VehicleState, now time.Time) float64 {
	return ComputeConfidence(state, now)
}

// sourceLabel returns a human-readable label for a SourceType, useful for
// logging and metrics.
func sourceLabel(s types.SourceType) string {
	switch s {
	case types.SourceRedis:
		return "redis"
	case types.SourceClickHouse:
		return "clickhouse"
	case types.SourceMerged:
		return "merged"
	default:
		return "unknown"
	}
}
