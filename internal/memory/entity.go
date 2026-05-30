package memory

import (
	"math"
	"time"

	"hypnotz/internal/trafficmodel"
	"hypnotz/internal/types"
)

// Position is a geographic coordinate stored on a MemoryEntity.
type Position struct {
	Lat float64
	Lon float64
}

// Velocity holds the kinematic state of a MemoryEntity.
type Velocity struct {
	Speed   float64
	Heading float64
	Dx      float64 // degrees-lat per second (derived)
	Dy      float64 // degrees-lon per second (derived)
}

const (
	DefaultTrajectoryMax = 64
	DefaultDecayRate     = 0.995
	AnomalySalienceBoost = 0.4
	BaseSalience         = 0.5

	// defaultSegmentSpeedMS is the assumed expected speed on a segment before
	// any speed samples have been collected. 15 m/s ≈ 54 km/h.
	defaultSegmentSpeedMS = trafficmodel.DefaultExpectedSpeedMS

	// anomalyRiskDelta is added to RiskScore each time a speed anomaly is
	// detected on a segment.
	anomalyRiskDelta = 0.2

	// salienceDecayTimeConst controls the exponential salience decay applied
	// during Apply. Lower values decay faster.
	salienceDecayTimeConst = 0.1
)

// MemoryEntity holds all persistent state for a single tracked vehicle.
type MemoryEntity struct {
	ID string

	// Current kinematic state.
	Position Position
	Velocity Velocity

	// Temporal memory.
	Trajectory    []Position
	TrajectoryMax int
	LastSeen      time.Time
	FirstSeen     time.Time
	SeenCount     int

	// Attention / salience.
	AttentionHistory []float64
	Salience         float64
	RiskScore        float64

	// Semantic memory.
	Embedding      types.Vector
	Classification string

	// Predictive state.
	PredictedPath []Position
	AnomalyCount  int

	// Persistence flags.
	IsStale   bool
	DecayRate float64

	// Traffic modelling.
	SpeedHistogram      *trafficmodel.SpeedHistogram
	PendingCrossings    map[int64]*trafficmodel.Crossing // segmentID → entry crossing
	LastSpeedSample     *trafficmodel.SpeedSample
	AverageSegmentSpeed float64 // expected speed for the current segment, m/s
}

// NewMemoryEntity constructs a MemoryEntity from the first observed event.
func NewMemoryEntity(id string, event types.VehicleState) *MemoryEntity {
	now := time.Now()
	e := &MemoryEntity{
		ID:                  id,
		Position:            Position{Lat: event.Lat, Lon: event.Lon},
		Velocity:            Velocity{Speed: event.Speed, Heading: event.Heading},
		Trajectory:          make([]Position, 0, DefaultTrajectoryMax),
		TrajectoryMax:       DefaultTrajectoryMax,
		LastSeen:            now,
		FirstSeen:           now,
		SeenCount:           1,
		AttentionHistory:    make([]float64, 0, 10),
		Salience:            BaseSalience,
		RiskScore:           0.0,
		Embedding:           make(types.Vector, 0),
		DecayRate:           DefaultDecayRate,
		IsStale:             false,
		SpeedHistogram:      trafficmodel.NewSpeedHistogram(),
		PendingCrossings:    make(map[int64]*trafficmodel.Crossing),
		LastSpeedSample:     nil,
		AverageSegmentSpeed: defaultSegmentSpeedMS,
	}
	e.Trajectory = append(e.Trajectory, e.Position)
	return e
}

// Apply updates the entity from a new observation.
func (m *MemoryEntity) Apply(event types.VehicleState) {
	dt := time.Since(m.LastSeen)
	dtSeconds := dt.Seconds()
	if dtSeconds < 0.001 {
		dtSeconds = 0.001
	}

	prev := m.Trajectory[len(m.Trajectory)-1]
	dx := event.Lat - prev.Lat
	dy := event.Lon - prev.Lon

	m.Position = Position{Lat: event.Lat, Lon: event.Lon}
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

	// Low-confidence or merged readings are treated as potential anomalies.
	isAnomalous := event.DataSource == types.SourceMerged || event.Confidence < 0.3
	if isAnomalous {
		m.AnomalyCount++
		m.Salience = math.Min(m.Salience+AnomalySalienceBoost, 1.0)
		m.RiskScore = math.Min(m.RiskScore+0.1, 1.0)
	}

	// Exponential salience decay over time.
	m.Salience *= math.Exp(-dtSeconds * salienceDecayTimeConst)
	if m.Salience < 0.01 {
		m.Salience = 0.01
	}

	m.IsStale = false
}

// Decay reduces salience and risk score by the configured decay rate raised
// to the power of dt (seconds elapsed).
func (m *MemoryEntity) Decay(dt float64) {
	factor := math.Pow(m.DecayRate, dt)
	m.Salience *= factor
	m.RiskScore *= factor
	if m.Salience < 0.01 {
		m.IsStale = true
	}
}

// ProcessTrafficCrossing feeds a TripLine crossing into the entity's traffic
// model. When a matching entry crossing already exists for the segment, a
// SpeedSample is computed and the histogram is updated.
func (m *MemoryEntity) ProcessTrafficCrossing(crossing *trafficmodel.Crossing) *trafficmodel.SpeedSample {
	segID := crossing.TripLine.SegmentID
	pending, hasPending := m.PendingCrossings[segID]

	if hasPending && pending.TripLine.Index < crossing.TripLine.Index {
		sample := trafficmodel.ComputeSpeed(pending, crossing)
		if sample != nil {
			m.SpeedHistogram.AddSample(sample.Time, sample.Speed)
			m.LastSpeedSample = sample

			_, mean, _ := m.SpeedHistogram.GetStats()
			m.AverageSegmentSpeed = mean

			// Flag speed anomaly when the sample deviates strongly from
			// the historical expected speed for this segment.
			expected := m.SpeedHistogram.GetExpectedSpeed(0)
			if sample.Speed > expected*1.5 || sample.Speed < expected*0.5 {
				m.RiskScore = math.Min(m.RiskScore+anomalyRiskDelta, 1.0)
			}
		}
		delete(m.PendingCrossings, segID)
		return sample
	}

	// Store entry crossing to wait for the paired exit crossing.
	if crossing.TripLine.Index == 1 {
		m.PendingCrossings[segID] = crossing
	}
	return nil
}

// GetSpeedDeviation returns how many standard deviations the most recent
// speed sample is from the segment's historical mean. Returns 0 when there
// is no sample or the standard deviation is zero.
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

// IsAnomalous reports whether this entity has accumulated enough anomaly
// signals to be considered abnormal.
func (m *MemoryEntity) IsAnomalous() bool {
	return m.AnomalyCount > 2 || m.RiskScore > 0.7
}

// Predict returns the estimated position after dt has elapsed, using the
// current linear velocity components.
func (m *MemoryEntity) Predict(dt time.Duration) Position {
	s := dt.Seconds()
	return Position{
		Lat: m.Position.Lat + m.Velocity.Dx*s,
		Lon: m.Position.Lon + m.Velocity.Dy*s,
	}
}

// GetTrajectory returns the recorded position history.
func (m *MemoryEntity) GetTrajectory() []Position { return m.Trajectory }

// GetVelocityMagnitude returns the Euclidean magnitude of the derived
// velocity components (degrees per second).
func (m *MemoryEntity) GetVelocityMagnitude() float64 {
	return math.Sqrt(m.Velocity.Dx*m.Velocity.Dx + m.Velocity.Dy*m.Velocity.Dy)
}

// RecordAttention appends a score to the rolling attention history (cap 10).
func (m *MemoryEntity) RecordAttention(score float64) {
	if len(m.AttentionHistory) >= 10 {
		m.AttentionHistory = m.AttentionHistory[1:]
	}
	m.AttentionHistory = append(m.AttentionHistory, score)
}

// GetAverageAttention returns the mean of the attention history.
func (m *MemoryEntity) GetAverageAttention() float64 {
	if len(m.AttentionHistory) == 0 {
		return 0.0
	}
	sum := 0.0
	for _, s := range m.AttentionHistory {
		sum += s
	}
	return sum / float64(len(m.AttentionHistory))
}

// GetStaleness returns how long ago this entity was last observed.
func (m *MemoryEntity) GetStaleness() time.Duration { return time.Since(m.LastSeen) }

// GetEmbedding returns the current embedding, computing it lazily if needed.
func (m *MemoryEntity) GetEmbedding() types.Vector {
	if len(m.Embedding) == 0 {
		m.UpdateEmbedding()
	}
	return m.Embedding
}

// UpdateEmbedding refreshes the embedding from the current entity state.
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
