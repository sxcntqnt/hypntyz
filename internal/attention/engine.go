package attention

import (
	"github.com/takara-ai/go-attention/attention"

	"hypnotz/internal/features"
	"hypnotz/internal/memory"
	"hypnotz/internal/types"
)

type Engine struct {
	model      *AttentionModel
	vectorPool *features.VectorPool
	config     AttentionConfig
}

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
	query[3] = 0.0
	query[4] = 0.0
	query[5] = 0.0
	query[6] = 0.0

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

func (e *Engine) ScoreEntity(entity *memory.MemoryEntity, client types.ClientState) float64 {
	embedding := entity.GetEmbedding()
	if len(embedding) == 0 {
		return 0.0
	}

	query := make(attention.Vector, len(embedding))
	query[0] = client.FocusLat
	query[1] = client.FocusLon

	viewportSize := (client.Viewport.MaxLat - client.Viewport.MinLat) *
		(client.Viewport.MaxLon - client.Viewport.MinLon)
	query[2] = 1.0 / (viewportSize + 0.001)

	if client.Preferences.AnomalyPriority && entity.IsAnomalous() {
		query[3] = 1.0
	}

	for i := 4; i < len(query) && i < len(embedding); i++ {
		query[i] = embedding[i]
	}

	keyVec := make(attention.Vector, len(embedding))
	for i, v := range embedding {
		keyVec[i] = v
	}
	
	keys := attention.Matrix{keyVec}
	values := attention.Matrix{keyVec}

	_, weights, err := e.model.Score(query, keys, values)
	if err != nil {
		return 0.0
	}

	score := 0.0
	if len(weights) > 0 {
		score = weights[0]
	}

	score = score*0.6 + entity.Salience*0.4

	if entity.IsAnomalous() {
		score += 0.15
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

func (e *Engine) ScoreBatch(vehicles []types.Vehicle, focusLat, focusLon float64) []float64 {
	scores := make([]float64, len(vehicles))
	for i, v := range vehicles {
		scores[i] = e.ScoreVehicle(v, focusLat, focusLon)
	}
	return scores
}

func (e *Engine) BuildClientQuery(client types.ClientState) attention.Vector {
	query := make(attention.Vector, 7)
	query[0] = client.FocusLat
	query[1] = client.FocusLon

	viewportSize := (client.Viewport.MaxLat - client.Viewport.MinLat) *
		(client.Viewport.MaxLon - client.Viewport.MinLon)
	query[2] = 1.0 / (viewportSize + 0.001)

	if client.Preferences.AnomalyPriority {
		query[3] = 1.0
	}

	return query
}

func (e *Engine) ProcessTensorSequence(seq types.TensorSequence) (attention.Matrix, error) {
	if len(seq.Tokens) == 0 {
		return attention.Matrix{}, nil
	}

	query := make(attention.Matrix, 1)
	query[0] = make(attention.Vector, len(seq.Tokens[0]))
	for i, val := range seq.Tokens[0] {
		query[0][i] = val
	}

	keys := make(attention.Matrix, len(seq.Tokens))
	for i, token := range seq.Tokens {
		keys[i] = make(attention.Vector, len(token))
		for j, val := range token {
			keys[i][j] = val
		}
	}

	values := keys

	output, err := e.model.Forward(query, keys, values)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (e *Engine) GetModel() *AttentionModel {
	return e.model
}

func (e *Engine) QueryMemory(client types.ClientState, entities []*memory.MemoryEntity) []float64 {
	if len(entities) == 0 {
		return []float64{}
	}

	scores := make([]float64, len(entities))
	for i, entity := range entities {
		scores[i] = e.ScoreEntity(entity, client)
	}

	return scores
}
