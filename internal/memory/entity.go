package memory

import (
	"math"
	"time"

	"hypnotz/internal/traffic"
	"hypnotz/internal/types"
)

type Position struct {
	Lat float64
	Lon float64
}

type Velocity struct {
	Speed    float64
	Heading  float64
	Dx       float64
	Dy       float64
}

type MemoryEntity struct {
	ID string

	// Current state
	Position Position
	Velocity Velocity

	// Temporal memory
	Trajectory      []Position
	TrajectoryMax   int
	LastSeen        time.Time
	FirstSeen       time.Time
	SeenCount       int

	// Attention history
	AttentionHistory []float64
	Salience         float64
	RiskScore        float64

	// Semantic memory
	Embedding    types.Vector
	Classification string

	// Predictive state
	PredictedPath []Position
	AnomalyCount  int

	// Persistence
	IsStale     bool
	DecayRate   float64

	// Traffic modeling
	SpeedHistogram *traffic.SpeedHistogram
	PendingCrossings map[int64]*traffic.Crossing // segmentID -> crossing
	LastSpeedSample *traffic.SpeedSample
	AverageSegmentSpeed float64 // Expected speed for current segment
}

const (
	DefaultTrajectoryMax = 64
	DefaultDecayRate     = 0.995
	AnomalySalienceBoost = 0.4
	BaseSalience         = 0.5
)

func NewMemoryEntity(id string, event types.VehicleState) *MemoryEntity {
	now := time.Now()

	entity := &MemoryEntity{
		ID:                 id,
		Position:           Position{Lat: event.Lat, Lon: event.Lon},
		Velocity:           Velocity{Speed: event.Speed, Heading: event.Heading},
		Trajectory:         make([]Position, 0, DefaultTrajectoryMax),
		TrajectoryMax:      DefaultTrajectoryMax,
		LastSeen:           now,
		FirstSeen:          now,
		SeenCount:          1,
		AttentionHistory:   make([]float64, 0, 10),
		Salience:           BaseSalience,
		RiskScore:          0.0,
		Embedding:          make(types.Vector, 0),
		DecayRate:          DefaultDecayRate,
		IsStale:            false,
		SpeedHistogram:     traffic.NewSpeedHistogram(),
		PendingCrossings:   make(map[int64]*traffic.Crossing),
		LastSpeedSample:    nil,
		AverageSegmentSpeed: 15.0, // Default 15 m/s
	}

	entity.Trajectory = append(entity.Trajectory, entity.Position)

	return entity
}

func (m *MemoryEntity) Apply(event types.VehicleState) {
	dt := time.Since(m.LastSeen)
	dtSeconds := dt.Seconds()
	if dtSeconds < 0.001 {
		dtSeconds = 0.001
	}

	m.Position = Position{Lat: event.Lat, Lon: event.Lon}

	dx := event.Lat - m.Trajectory[len(m.Trajectory)-1].Lat
	dy := event.Lon - m.Trajectory[len(m.Trajectory)-1].Lon

	m.Velocity = Velocity{
		Speed:   event.Speed,
		Heading: event.Heading,
		Dx:      dx / dtSeconds,
		Dy:      dy / dtSeconds,
	}

	if len(m.Trajectory) >= m.TrajectoryMax {
		m.Trajectory = m.Trajectory[1:]
	}
	m.Trajectory = append(m.Trajectory, m.Position)

	m.LastSeen = time.Now()
	m.SeenCount++

	isAnomalous := event.DataSource == "merged" || event.Confidence < 0.3
	if isAnomalous {
		m.AnomalyCount++
		m.Salience += AnomalySalienceBoost
		if m.Salience > 1.0 {
			m.Salience = 1.0
		}
		m.RiskScore += 0.1
		if m.RiskScore > 1.0 {
			m.RiskScore = 1.0
		}
	}

	m.Salience *= math.Exp(-dtSeconds * 0.1)
	if m.Salience < 0.01 {
		m.Salience = 0.01
	}

	m.IsStale = false
}

func (m *MemoryEntity) Decay(dt float64) {
	decayFactor := math.Pow(m.DecayRate, dt)
	m.Salience *= decayFactor
	m.RiskScore *= decayFactor

	if m.Salience < 0.01 {
		m.IsStale = true
	}
}

func (m *MemoryEntity) GetEmbedding() types.Vector {
	if len(m.Embedding) == 0 {
		m.Embedding = types.Vector{
			m.Position.Lat,
			m.Position.Lon,
			m.Velocity.Speed,
			m.Velocity.Heading,
			m.Salience,
			float64(m.AnomalyCount),
			m.RiskScore,
		}
	}
	return m.Embedding
}

func (m *MemoryEntity) UpdateEmbedding() {
	m.Embedding = types.Vector{
		m.Position.Lat,
		m.Position.Lon,
		m.Velocity.Speed,
		m.Velocity.Heading,
		m.Salience,
		float64(m.AnomalyCount),
		m.RiskScore,
	}
}

func (m *MemoryEntity) Predict(dt time.Duration) Position {
	dtSeconds := dt.Seconds()
	return Position{
		Lat: m.Position.Lat + m.Velocity.Dx * dtSeconds,
		Lon: m.Position.Lon + m.Velocity.Dy * dtSeconds,
	}
}

func (m *MemoryEntity) GetTrajectory() []Position {
	return m.Trajectory
}

func (m *MemoryEntity) GetVelocityMagnitude() float64 {
	return math.Sqrt(m.Velocity.Dx*m.Velocity.Dx + m.Velocity.Dy*m.Velocity.Dy)
}

func (m *MemoryEntity) RecordAttention(score float64) {
	if len(m.AttentionHistory) >= 10 {
		m.AttentionHistory = m.AttentionHistory[1:]
	}
	m.AttentionHistory = append(m.AttentionHistory, score)
}

func (m *MemoryEntity) GetAverageAttention() float64 {
	if len(m.AttentionHistory) == 0 {
		return 0.0
	}
	sum := 0.0
	for _, score := range m.AttentionHistory {
		sum += score
	}
	return sum / float64(len(m.AttentionHistory))
}

func (m *MemoryEntity) IsAnomalous() bool {
	return m.AnomalyCount > 2 || m.RiskScore > 0.7
}

func (m *MemoryEntity) GetStaleness() time.Duration {
	return time.Since(m.LastSeen)
}

// ProcessTrafficCrossing processes a TripLine crossing and potentially generates a speed sample
func (m *MemoryEntity) ProcessTrafficCrossing(crossing *traffic.Crossing) *traffic.SpeedSample {
	// Check if this crossing completes a pending crossing
	lastCrossing, exists := m.PendingCrossings[crossing.TripLine.SegmentID]
	
	if exists {
		// Verify direction (entry -> exit)
		if lastCrossing.TripLine.Index < crossing.TripLine.Index {
			// Compute speed sample
			sample := traffic.ComputeSpeed(lastCrossing, crossing)
			
			if sample != nil {
				// Add to histogram
				m.SpeedHistogram.AddSample(sample.Time, sample.Speed)
				m.LastSpeedSample = sample
				
				// Update average segment speed
				_, mean, _ := m.SpeedHistogram.GetStats()
				m.AverageSegmentSpeed = mean
				
				// Check for anomaly (deviation from expected speed)
				expected := m.SpeedHistogram.GetExpectedSpeed(0) // Simplified
				if sample.Speed > expected*1.5 || sample.Speed < expected*0.5 {
					m.RiskScore += 0.2
					if m.RiskScore > 1.0 {
						m.RiskScore = 1.0
					}
				}
			}
			
			// Clear pending crossing
			delete(m.PendingCrossings, crossing.TripLine.SegmentID)
			return sample
		}
	}
	
	// Store as pending crossing (entry tripline)
	if crossing.TripLine.Index == 1 {
		m.PendingCrossings[crossing.TripLine.SegmentID] = crossing
	}
	
	return nil
}

// GetSpeedDeviation returns how many standard deviations the current speed is from the mean
func (m *MemoryEntity) GetSpeedDeviation() float64 {
	if m.LastSpeedSample == nil {
		return 0.0
	}
	
	_, mean, stddev := m.SpeedHistogram.GetStats()
	if stddev == 0 {
		return 0.0
	}
	
	return (m.LastSpeedSample.Speed - mean) / stddev
}
