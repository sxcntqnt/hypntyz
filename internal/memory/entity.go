package memory

import (
	"math"
	"time"

	"hypnotz/internal/spectral"
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

	defaultSegmentSpeedMS  = trafficmodel.DefaultExpectedSpeedMS
	anomalyRiskDelta       = 0.2
	salienceDecayTimeConst = 0.1
	entropySpikeSigma      = 2.0

	// spectralCrossingThreshold is the SpectralDeviationScore above which a
	// crossing event triggers an additional risk-score boost.
	spectralCrossingThreshold = 0.6

	// spectralCrossingRiskDelta is the risk increment applied when a crossing
	// event has a high spectral deviation score.
	spectralCrossingRiskDelta = 0.1
)

// MemoryEntity holds all persistent state for a single tracked vehicle.
type MemoryEntity struct {
	ID string

	Position Position
	Velocity Velocity

	Trajectory    []Position
	TrajectoryMax int
	LastSeen      time.Time
	FirstSeen     time.Time
	SeenCount     int

	AttentionHistory []float64
	Salience         float64
	RiskScore        float64

	Embedding      types.Vector
	Classification string

	PredictedPath []Position
	AnomalyCount  int

	IsStale   bool
	DecayRate float64

	// Traffic modelling.
	SpeedHistogram      *trafficmodel.SpeedHistogram
	PendingCrossings    map[int64]*trafficmodel.Crossing
	LastSpeedSample     *trafficmodel.SpeedSample
	AverageSegmentSpeed float64

	// Spectral cognition.
	SpeedHistory    *spectral.RingBuffer
	SpectralProfile *spectral.SpectralProfile
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
		SpeedHistory:        spectral.NewRingBuffer(spectral.DefaultWindowSize),
		SpectralProfile:     spectral.NewSpectralProfile(),
	}
	e.Trajectory = append(e.Trajectory, e.Position)
	return e
}

// Apply updates the entity from a new observation and refreshes the spectral
// profile when the speed ring buffer has enough samples.
func (m *MemoryEntity) Apply(event types.VehicleState) {
	dt := time.Since(m.LastSeen)
	dtSeconds := dt.Seconds()
	if dtSeconds < 0.001 {
		dtSeconds = 0.001
	}

	prev := m.Trajectory[len(m.Trajectory)-1]
	m.Position = Position{Lat: event.Lat, Lon: event.Lon}
	m.Velocity = Velocity{
		Speed:   event.Speed,
		Heading: event.Heading,
		Dx:      (event.Lat - prev.Lat) / dtSeconds,
		Dy:      (event.Lon - prev.Lon) / dtSeconds,
	}

	if len(m.Trajectory) >= m.TrajectoryMax {
		m.Trajectory = m.Trajectory[1:]
	}
	m.Trajectory = append(m.Trajectory, m.Position)
	m.LastSeen = time.Now()
	m.SeenCount++

	// Push speed sample and refresh spectral profile.
	m.SpeedHistory.Push(event.Speed)
	if m.SpeedHistory.Len() >= spectral.DefaultMinSamples {
		m.updateSpectral()
	}

	isAnomalous := event.DataSource == types.SourceMerged || event.Confidence < 0.3
	if isAnomalous {
		m.AnomalyCount++
		m.Salience = math.Min(m.Salience+AnomalySalienceBoost, 1.0)
		m.RiskScore = math.Min(m.RiskScore+0.1, 1.0)
	}

	if m.SpectralProfile.EntropySpike(entropySpikeSigma) {
		m.Salience = math.Min(m.Salience+0.15, 1.0)
	}

	m.Salience *= math.Exp(-dtSeconds * salienceDecayTimeConst)
	if m.Salience < 0.01 {
		m.Salience = 0.01
	}
	m.IsStale = false
}

// updateSpectral runs the FFT on the current speed ring buffer and refreshes
// SpectralProfile. Called from Apply while the store's write lock is held.
func (m *MemoryEntity) updateSpectral() {
	speeds := m.SpeedHistory.Slice()
	sp := spectral.Compute(speeds, spectral.DefaultSampleRate)
	m.SpectralProfile.Update(sp, time.Now().UnixNano())

	if float64(m.SpectralProfile.AnomalyScore) > 0.7 {
		m.RiskScore = math.Min(m.RiskScore+0.05, 1.0)
	}
}

// ProcessTrafficCrossing feeds a TripLine crossing into the traffic model.
// When a matching entry crossing exists, a SpeedSample is computed and its
// SpectralDeviationScore is populated from the current speed ring buffer.
func (m *MemoryEntity) ProcessTrafficCrossing(crossing *trafficmodel.Crossing) *trafficmodel.SpeedSample {
	segID := crossing.TripLine.SegmentID
	pending, hasPending := m.PendingCrossings[segID]

	if hasPending && pending.TripLine.Index < crossing.TripLine.Index {
		sample := trafficmodel.ComputeSpeed(pending, crossing)
		if sample != nil {
			// Populate spectral deviation score from the current speed window.
			// This enriches the sample with frequency-domain context at the
			// moment of the crossing, without requiring trafficmodel to import
			// the spectral package.
			if m.SpeedHistory.Len() >= spectral.DefaultMinSamples {
				speeds := m.SpeedHistory.Slice()
				sp := spectral.Compute(speeds, spectral.DefaultSampleRate)
				features := spectral.Extract(sp)
				sample.SpectralDeviationScore = float64(spectral.ComputeAnomalyScore(features))

				if sample.IsSpectrallyAnomalous(spectralCrossingThreshold) {
					m.RiskScore = math.Min(m.RiskScore+spectralCrossingRiskDelta, 1.0)
				}
			}

			m.SpeedHistogram.AddSample(sample.Time, sample.Speed)
			m.LastSpeedSample = sample

			_, mean, _ := m.SpeedHistogram.GetStats()
			m.AverageSegmentSpeed = mean

			expected := m.SpeedHistogram.GetExpectedSpeed(0)
			if sample.Speed > expected*1.5 || sample.Speed < expected*0.5 {
				m.RiskScore = math.Min(m.RiskScore+anomalyRiskDelta, 1.0)
			}
		}
		delete(m.PendingCrossings, segID)
		return sample
	}

	if crossing.TripLine.Index == 1 {
		m.PendingCrossings[segID] = crossing
	}
	return nil
}

// GetSpeedDeviation returns standard deviations from the segment mean.
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

func (m *MemoryEntity) IsAnomalous() bool {
	return m.AnomalyCount > 2 || m.RiskScore > 0.7
}

func (m *MemoryEntity) Decay(dt float64) {
	factor := math.Pow(m.DecayRate, dt)
	m.Salience *= factor
	m.RiskScore *= factor
	if m.Salience < 0.01 {
		m.IsStale = true
	}
}

func (m *MemoryEntity) Predict(dt time.Duration) Position {
	s := dt.Seconds()
	return Position{
		Lat: m.Position.Lat + m.Velocity.Dx*s,
		Lon: m.Position.Lon + m.Velocity.Dy*s,
	}
}

func (m *MemoryEntity) GetTrajectory() []Position { return m.Trajectory }

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
	for _, s := range m.AttentionHistory {
		sum += s
	}
	return sum / float64(len(m.AttentionHistory))
}

func (m *MemoryEntity) GetStaleness() time.Duration { return time.Since(m.LastSeen) }

func (m *MemoryEntity) GetEmbedding() types.Vector {
	if len(m.Embedding) == 0 {
		m.UpdateEmbedding()
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
