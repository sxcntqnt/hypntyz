package attention

import (
	"github.com/takara-ai/go-attention/attention"

	"hypnotz/internal/features"
	"hypnotz/internal/memory"
	"hypnotz/internal/types"
)

// Attention scoring weights.
// The three components must sum to 1.0.
const (
	weightGeometric = 0.50 // dot-product attention score (spatial relevance)
	weightSalience  = 0.30 // entity salience (accumulated over time)
	weightSpectral  = 0.20 // spectral anomaly score (frequency-domain signal)

	// anomalyBoost is added after blending when the entity's discrete
	// anomaly signals (AnomalyCount / RiskScore threshold) fire.
	anomalyBoost = 0.15
)

// Engine scores MemoryEntities and raw Vehicles for a given client state.
type Engine struct {
	model      *AttentionModel
	vectorPool *features.VectorPool
	config     AttentionConfig
}

// NewEngine constructs an Engine. Returns nil when the model cannot be
// initialised with cfg (e.g. invalid head configuration).
func NewEngine(cfg AttentionConfig, pool *features.VectorPool) *Engine {
	model, err := NewAttentionModel(cfg)
	if err != nil {
		return nil
	}
	return &Engine{
		model:      model,
		vectorPool: pool,
		config:     cfg,
	}
}

// ScoreEntity computes the attention score for entity from the perspective of
// client. The score blends geometric relevance, entity salience, and spectral
// anomaly signal according to the package-level weight constants.
//
// Score formula:
//
//	attention_score * weightGeometric
//	+ entity.Salience * weightSalience
//	+ spectral_anomaly * weightSpectral
//	+ anomalyBoost (when entity.IsAnomalous())
func (e *Engine) ScoreEntity(entity *memory.MemoryEntity, client types.ClientState) float64 {
	embedding := entity.GetEmbedding()
	if len(embedding) == 0 {
		return 0.0
	}

	query := make(attention.Vector, len(embedding))
	query[0] = client.FocusLat
	query[1] = client.FocusLon

	viewportSize := (client.Viewport.MaxLat-client.Viewport.MinLat) *
		(client.Viewport.MaxLon - client.Viewport.MinLon)
	query[2] = 1.0 / (viewportSize + 0.001)

	if client.Preferences.AnomalyPriority && entity.IsAnomalous() {
		query[3] = 1.0
	}
	for i := 4; i < len(query) && i < len(embedding); i++ {
		query[i] = embedding[i]
	}

	keyVec := make(attention.Vector, len(embedding))
	copy(keyVec, embedding)

	keys := attention.Matrix{keyVec}
	values := attention.Matrix{keyVec}

	_, weights, err := e.model.Score(query, keys, values)
	if err != nil {
		return 0.0
	}

	geoScore := 0.0
	if len(weights) > 0 {
		geoScore = weights[0]
	}

	// Spectral component: pull AnomalyScore from the entity's profile.
	spectralScore := 0.0
	if entity.SpectralProfile != nil {
		spectralScore = float64(entity.SpectralProfile.AnomalyScore)
	}

	score := geoScore*weightGeometric +
		entity.Salience*weightSalience +
		spectralScore*weightSpectral

	if entity.IsAnomalous() {
		score += anomalyBoost
	}

	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}

	entity.RecordAttention(score)
	return score
}

// ScoreVehicle scores a raw Vehicle (no entity state) against a focus point.
// Used for initial ranking before entities are fully populated.
func (e *Engine) ScoreVehicle(vehicle types.Vehicle, focusLat, focusLon float64) float64 {
	builder := features.NewBuilder(e.vectorPool)
	fv := builder.Build(vehicle, focusLat, focusLon)

	key := e.vectorPool.Get()
	key[0] = fv.Lat
	key[1] = fv.Lon
	key[2] = fv.Velocity
	key[3] = fv.SinHeading
	key[4] = fv.CosHeading
	key[5] = fv.Distance
	key[6] = fv.AnomalyScore

	query := make(attention.Vector, 7)
	query[0] = focusLat
	query[1] = focusLon
	query[2] = 1.0

	keys := attention.Matrix{key}
	values := attention.Matrix{key}

	_, weights, err := e.model.Score(query, keys, values)
	if err != nil {
		return 0.0
	}

	score := 0.0
	if len(weights) > 0 {
		score = weights[0]
	}

	if fv.AnomalyScore > 0.5 {
		score += 0.2
		if score > 1.0 {
			score = 1.0
		}
	}
	return score
}

// ScoreBatch scores a slice of raw vehicles against a focus point.
func (e *Engine) ScoreBatch(vehicles []types.Vehicle, focusLat, focusLon float64) []float64 {
	scores := make([]float64, len(vehicles))
	for i, v := range vehicles {
		scores[i] = e.ScoreVehicle(v, focusLat, focusLon)
	}
	return scores
}

// QueryMemory scores all entities in the slice for client.
func (e *Engine) QueryMemory(client types.ClientState, entities []*memory.MemoryEntity) []float64 {
	scores := make([]float64, len(entities))
	for i, entity := range entities {
		scores[i] = e.ScoreEntity(entity, client)
	}
	return scores
}

// BuildClientQuery builds the 7-dimensional query vector for a client.
func (e *Engine) BuildClientQuery(client types.ClientState) attention.Vector {
	query := make(attention.Vector, 7)
	query[0] = client.FocusLat
	query[1] = client.FocusLon
	viewportSize := (client.Viewport.MaxLat-client.Viewport.MinLat) *
		(client.Viewport.MaxLon - client.Viewport.MinLon)
	query[2] = 1.0 / (viewportSize + 0.001)
	if client.Preferences.AnomalyPriority {
		query[3] = 1.0
	}
	return query
}

// ProcessTensorSequence applies multi-head attention to a full TensorSequence.
func (e *Engine) ProcessTensorSequence(seq types.TensorSequence) (attention.Matrix, error) {
	if len(seq.Tokens) == 0 {
		return attention.Matrix{}, nil
	}

	query := make(attention.Matrix, 1)
	query[0] = make(attention.Vector, len(seq.Tokens[0]))
	copy(query[0], seq.Tokens[0])

	keys := make(attention.Matrix, len(seq.Tokens))
	for i, token := range seq.Tokens {
		keys[i] = make(attention.Vector, len(token))
		copy(keys[i], token)
	}

	return e.model.Forward(query, keys, keys)
}

// GetModel returns the underlying AttentionModel.
func (e *Engine) GetModel() *AttentionModel { return e.model }
