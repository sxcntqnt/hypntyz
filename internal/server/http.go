package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"hypnotz/internal/attention"
	"hypnotz/internal/engine"
	"hypnotz/internal/features"
	"hypnotz/internal/ranker"
	"hypnotz/internal/sirtebasin"
	stream2 "hypnotz/internal/stream"
	"hypnotz/internal/types"
)

type Server struct {
	httpServer    *http.Server
	streamHub     *stream2.Hub
	attentionEng  *attention.Engine
	sirtebasinCli *sirtebasin.Client
	projEngine    *engine.ProjectionEngine
	rank          *ranker.Ranker
	vectorPool    *features.VectorPool
	config        types.ClientConfig
	tickStop      chan struct{}
	tickWg        sync.WaitGroup
	scored        []ranker.ScoredVehicle
}

func NewServer() *Server {
	config := loadConfig()

	engineCfg := engine.EngineConfig{
		TickRateHz:           config.TickRateHz,
		MaxVehiclesPerClient: config.MaxVehiclesPerClient,
		MaxClientsPerNode:    config.MaxClientsPerNode,
		EnableBackpressure:   config.EnableBackpressure,
		RegionID:             config.RegionID,
		SirtebasinURL:        config.SirtebasinURL,
		RedisURL:             config.RedisURL,
		ClickHouseHost:       os.Getenv("CLICKHOUSE_HOST"),
	}

	projEngine, err := engine.NewProjectionEngine(engineCfg)
	if err != nil {
		log.Printf("Warning: Could not initialize projection engine: %v", err)
	}

	vectorPool := features.NewVectorPool(features.FeatureSize)
	attentionCfg := attention.DefaultAttentionConfig()
	attentionEng := attention.NewEngine(attentionCfg, vectorPool)
	sirtebasinCli := sirtebasin.New(config.SirtebasinURL)
	streamHub := stream2.NewHub(config.MaxClientsPerNode, config.EnableBackpressure)
	rank := ranker.NewRanker(config.MaxVehiclesPerClient)

	s := &Server{
		streamHub:     streamHub,
		attentionEng:  attentionEng,
		sirtebasinCli: sirtebasinCli,
		projEngine:    projEngine,
		rank:          rank,
		vectorPool:    vectorPool,
		config:        config,
		tickStop:      make(chan struct{}),
	}

	return s
}

func loadConfig() types.ClientConfig {
	cfg := types.DefaultClientConfig()

	if val := os.Getenv("SIRTEBASIN_URL"); val != "" {
		cfg.SirtebasinURL = val
	}
	if val := os.Getenv("REDIS_URL"); val != "" {
		cfg.RedisURL = val
	}
	if val := os.Getenv("TICK_RATE_HZ"); val != "" {
		if hz, err := strconv.Atoi(val); err == nil && hz > 0 {
			cfg.TickRateHz = hz
		}
	}
	if val := os.Getenv("MAX_VEHICLES_PER_CLIENT"); val != "" {
		if max, err := strconv.Atoi(val); err == nil && max > 0 {
			cfg.MaxVehiclesPerClient = max
		}
	}
	if val := os.Getenv("MAX_CLIENTS_PER_NODE"); val != "" {
		if max, err := strconv.Atoi(val); err == nil && max > 0 {
			cfg.MaxClientsPerNode = max
		}
	}
	if val := os.Getenv("ENABLE_BACKPRESSURE"); val != "" {
		cfg.EnableBackpressure = val == "true" || val == "1"
	}
	if val := os.Getenv("REGION_ID"); val != "" {
		cfg.RegionID = val
	}

	return cfg
}

func (s *Server) Run(addr string) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	log.Printf("Projection Engine starting on %s", addr)
	log.Printf("Configuration: TickRate=%dHz, MaxVehicles=%d, MaxClients=%d, Backpressure=%v",
		s.config.TickRateHz, s.config.MaxVehiclesPerClient, s.config.MaxClientsPerNode, s.config.EnableBackpressure)

	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	close(s.tickStop)
	s.tickWg.Wait()

	if s.projEngine != nil {
		s.projEngine.Stop()
	}

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) StartTickLoop() {
	s.tickWg.Add(1)
	go func() {
		defer s.tickWg.Done()

		tickDuration := time.Duration(1000/s.config.TickRateHz) * time.Millisecond
		ticker := time.NewTicker(tickDuration)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.tick()
			case <-s.tickStop:
				return
			}
		}
	}()
}

func (s *Server) tick() {
	startTime := time.Now()

	clients := s.streamHub.GetAllClients()
	if len(clients) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	for _, client := range clients {
		var projections []types.Projection
		var err error

		if s.projEngine != nil {
			projections, err = s.projEngine.ProcessTick(ctx, client.State)
			if err != nil {
				log.Printf("Projection engine error: %v", err)
			}
		}

		if projections == nil {
			vehicles, err := s.sirtebasinCli.QueryViewport(
				ctx,
				client.State.Viewport.MinLat,
				client.State.Viewport.MinLon,
				client.State.Viewport.MaxLat,
				client.State.Viewport.MaxLon,
			)

			if err != nil {
				log.Printf("Error fetching vehicles for client %s: %v", client.ID, err)
				continue
			}

			projections = s.processClientVehicles(client.ID, client.State, vehicles)
		}

		batch := types.ProjectionBatch{
			ClientID:  client.ID,
			Timestamp: time.Now().Unix(),
			Vehicles:  projections,
		}

		stream2.SendToClient(s.streamHub, client.ID, batch)
	}

	elapsed := time.Since(startTime)
	if elapsed > 50*time.Millisecond {
		log.Printf("Tick took %v (threshold: 50ms)", elapsed)
	}
}

func (s *Server) processClientVehicles(clientID string, clientState types.ClientState, vehicles []types.Vehicle) []types.Projection {
	scores := make([]float64, len(vehicles))
	for i, v := range vehicles {
		scores[i] = s.attentionEng.ScoreVehicle(v, clientState.FocusLat, clientState.FocusLon)
	}

	s.scored = s.rank.RankAndThin(vehicles, scores, clientState.MaxResults)

	return s.rank.ToProjections(s.scored)
}

type SubscribeRequest struct {
	Viewport    types.Viewport    `json:"viewport"`
	FocusLat    float64           `json:"focus_lat"`
	FocusLon    float64           `json:"focus_lon"`
	Preferences types.Preferences `json:"preferences"`
	MaxResults  int               `json:"max_results"`
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		clientID = fmt.Sprintf("client_%d", time.Now().UnixNano())
	}

	if req.MaxResults <= 0 {
		req.MaxResults = 500
	}

	state := types.ClientState{
		ID: clientID,
		Viewport: types.Viewport{
			MinLat: req.Viewport.MinLat,
			MinLon: req.Viewport.MinLon,
			MaxLat: req.Viewport.MaxLat,
			MaxLon: req.Viewport.MaxLon,
		},
		FocusLat:    req.FocusLat,
		FocusLon:    req.FocusLon,
		Preferences: req.Preferences,
		MaxResults:  req.MaxResults,
	}

	conn := s.streamHub.AddClient(clientID, state)
	if conn == nil {
		http.Error(w, "Server at capacity", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"client_id": clientID,
		"status":    "subscribed",
	})
}
