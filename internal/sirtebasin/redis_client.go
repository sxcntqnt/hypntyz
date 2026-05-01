package sirtebasin

import (
	"context"
	"fmt"
	"time"

	"hypnotz/internal/types"
)

type RedisClient struct {
	// Redis connection would go here
	// For now, this is a stub that will be filled when Redis is available
}

func NewRedisClient(url string) (*RedisClient, error) {
	return &RedisClient{}, nil
}

func (r *RedisClient) QueryRedis(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	if req.VehicleID == "" {
		return r.queryAllVehicles(ctx, req)
	}
	return r.queryVehicle(ctx, req)
}

func (r *RedisClient) queryVehicle(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	key := fmt.Sprintf("vehicle:%s", req.VehicleID)
	_ = key

	states := make([]types.VehicleState, 0)

	now := time.Now()
	if req.TimeRange.Start.IsZero() {
		req.TimeRange.Start = now.Add(-5 * time.Minute)
	}
	if req.TimeRange.End.IsZero() {
		req.TimeRange.End = now
	}

	return states, nil
}

func (r *RedisClient) queryAllVehicles(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error) {
	states := make([]types.VehicleState, 0)
	return states, nil
}

func (r *RedisClient) GetLatestState(ctx context.Context, vehicleID string) (*types.VehicleState, error) {
	key := fmt.Sprintf("vehicle:%s", vehicleID)
	_ = key

	return nil, nil
}

func (r *RedisClient) GetStatesInWindow(ctx context.Context, vehicleID string, start, end time.Time) ([]types.VehicleState, error) {
	states := make([]types.VehicleState, 0)
	return states, nil
}

func (r *RedisClient) computeConfidence(state types.VehicleState, now time.Time) float64 {
	age := now.Sub(state.Timestamp)
	maxAge := 300 * time.Second

	base := 1.0 - float64(age)/float64(maxAge)

	var sourceBonus float64
	switch state.DataSource {
	case types.Redis:
		sourceBonus = 0.15
	case types.ClickHouse:
		sourceBonus = 0.10
	case types.Merged:
		sourceBonus = 0.05
	}

	score := base + sourceBonus

	if score < 0.0 {
		return 0.0
	}
	if score > 1.0 {
		return 1.0
	}

	return score
}
