package sirtebasin

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"hypnotz/internal/types"
)

type StorageClient interface {
	QueryRedis(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error)
	QueryClickHouse(ctx context.Context, req types.QueryRequest) ([]types.VehicleState, error)
}

type MergeEngine interface {
	Merge(redis []types.VehicleState, clickhouse []types.VehicleState) []types.VehicleState
}

type SirtebasinAdapter struct {
	redis        *RedisClient
	clickhouse   *ClickHouseClient
	mergeEngine  MergeEngine
	config       AdapterConfig
}

type AdapterConfig struct {
	ClickHouseTimeout time.Duration
	StabilityThreshold time.Duration
	RedisURL         string
	ClickHouseHost   string
}

func DefaultAdapterConfig() AdapterConfig {
	return AdapterConfig{
		ClickHouseTimeout:  200 * time.Millisecond,
		StabilityThreshold: 5 * time.Minute,
	}
}

func NewAdapter(cfg AdapterConfig) (*SirtebasinAdapter, error) {
	redisClient, err := NewRedisClient(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("redis client: %w", err)
	}

	chClient, err := NewClickHouseClient(cfg.ClickHouseHost)
	if err != nil {
		return nil, fmt.Errorf("clickhouse client: %w", err)
	}

	return &SirtebasinAdapter{
		redis:       redisClient,
		clickhouse:  chClient,
		mergeEngine: NewMergeEngine(),
		config:      cfg,
	}, nil
}

func (a *SirtebasinAdapter) Query(ctx context.Context, req types.QueryRequest) (types.VehicleStateSet, error) {
	switch req.Mode {
	case types.RealtimeOnly:
		return a.queryRealtimeOnly(ctx, req)
	case types.HistoricalOnly:
		return a.queryHistoricalOnly(ctx, req)
	case types.HybridReconcile:
		return a.queryHybrid(ctx, req)
	default:
		return types.VehicleStateSet{}, fmt.Errorf("invalid query mode: %s", req.Mode)
	}
}

func (a *SirtebasinAdapter) queryRealtimeOnly(ctx context.Context, req types.QueryRequest) (types.VehicleStateSet, error) {
	redisStates, err := a.redis.QueryRedis(ctx, req)
	if err != nil {
		return types.VehicleStateSet{
			StaleRealtime: true,
		}, fmt.Errorf("redis query failed: %w", err)
	}

	if len(redisStates) == 0 {
		return types.VehicleStateSet{
			StaleRealtime: true,
		}, nil
	}

	sortStatesByTimestamp(redisStates)

	return types.VehicleStateSet{
		QueryID:         req.QueryID,
		TimestampNS:     time.Now().UnixNano(),
		Vehicles:        redisStates,
		StaleRealtime:   false,
		PartialHistorical: false,
	}, nil
}

func (a *SirtebasinAdapter) queryHistoricalOnly(ctx context.Context, req types.QueryRequest) (types.VehicleStateSet, error) {
	chStates, err := a.clickhouse.QueryClickHouse(ctx, req)
	if err != nil {
		return types.VehicleStateSet{}, fmt.Errorf("clickhouse query failed: %w", err)
	}

	if len(chStates) == 0 {
		return types.VehicleStateSet{
			QueryID: req.QueryID,
		}, nil
	}

	sortStatesByTimestamp(chStates)

	return types.VehicleStateSet{
		QueryID:         req.QueryID,
		TimestampNS:     time.Now().UnixNano(),
		Vehicles:        chStates,
		PartialHistorical: false,
		StaleRealtime:   false,
	}, nil
}

func (a *SirtebasinAdapter) queryHybrid(ctx context.Context, req types.QueryRequest) (types.VehicleStateSet, error) {
	redisCh := make(chan []types.VehicleState, 1)
	errCh := make(chan error, 1)

	go func() {
		states, err := a.redis.QueryRedis(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		redisCh <- states
	}()

	var redisStates []types.VehicleState
	select {
	case redisStates = <-redisCh:
	case err := <-errCh:
		return types.VehicleStateSet{
			StaleRealtime: true,
		}, fmt.Errorf("redis query failed: %w", err)
	case <-ctx.Done():
		return types.VehicleStateSet{}, ctx.Err()
	}

	chTimeoutCtx, cancel := context.WithTimeout(ctx, a.config.ClickHouseTimeout)
	defer cancel()

	chStates, err := a.clickhouse.QueryClickHouse(chTimeoutCtx, req)
	partialHistorical := false

	if err != nil {
		partialHistorical = true
		chStates = []types.VehicleState{}
	}

	var merged []types.VehicleState
	if len(chStates) > 0 {
		merged = a.mergeEngine.Merge(redisStates, chStates)
	} else {
		merged = redisStates
	}

	if len(merged) == 0 {
		return types.VehicleStateSet{
			QueryID:           req.QueryID,
			TimestampNS:       time.Now().UnixNano(),
			PartialHistorical: partialHistorical,
			StaleRealtime:     true,
		}, nil
	}

	sortStatesByTimestamp(merged)

	return types.VehicleStateSet{
		QueryID:           req.QueryID,
		TimestampNS:       time.Now().UnixNano(),
		Vehicles:          merged,
		PartialHistorical: partialHistorical,
		StaleRealtime:     false,
	}, nil
}

func sortStatesByTimestamp(states []types.VehicleState) {
	sort.Slice(states, func(i, j int) bool {
		return states[i].TimestampNS < states[j].TimestampNS
	})
}

type ConcurrentQueryResult struct {
	States []types.VehicleState
	Error  error
}

type QueryTask struct {
	ctx  context.Context
	req  types.QueryRequest
	done chan struct{}
}

type WorkerPool struct {
	workers int
	tasks   chan QueryTask
	wg      sync.WaitGroup
}

func NewWorkerPool(workers int) *WorkerPool {
	return &WorkerPool{
		workers: workers,
		tasks:   make(chan QueryTask, 100),
	}
}

func (wp *WorkerPool) Start(adapter *SirtebasinAdapter) {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go func() {
			defer wp.wg.Done()
			for task := range wp.tasks {
				_, _ = adapter.Query(task.ctx, task.req)
				close(task.done)
			}
		}()
	}
}

func (wp *WorkerPool) Stop() {
	close(wp.tasks)
	wp.wg.Wait()
}
