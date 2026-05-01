package attention

import (
	"github.com/takara-ai/go-attention/attention"
)

type AttentionModel struct {
	multiHead *attention.MultiHeadAttention
	config    attention.MultiHeadConfig
}

type AttentionConfig struct {
	NumHeads    int
	DModel      int
	DKey        int
	DValue      int
	DropoutRate float64
}

func DefaultAttentionConfig() AttentionConfig {
	return AttentionConfig{
		NumHeads:    4,
		DModel:      64,
		DKey:        16,
		DValue:      16,
		DropoutRate: 0.1,
	}
}

func NewAttentionModel(cfg AttentionConfig) (*AttentionModel, error) {
	mhCfg := attention.MultiHeadConfig{
		NumHeads:    cfg.NumHeads,
		DModel:      cfg.DModel,
		DKey:        cfg.DKey,
		DValue:      cfg.DValue,
		DropoutRate: cfg.DropoutRate,
	}

	mha, err := attention.NewMultiHeadAttention(mhCfg)
	if err != nil {
		return nil, err
	}

	return &AttentionModel{
		multiHead: mha,
		config:    mhCfg,
	}, nil
}

func (m *AttentionModel) Score(query attention.Vector, keys, values attention.Matrix) (attention.Vector, attention.Vector, error) {
	output, weights, err := attention.DotProductAttention(query, keys, values)
	if err != nil {
		return nil, nil, err
	}

	return output, weights, nil
}

func (m *AttentionModel) Forward(query, key, value attention.Matrix) (attention.Matrix, error) {
	if m.multiHead == nil {
		return nil, nil
	}

	output, err := m.multiHead.Forward(query, key, value)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (m *AttentionModel) GetConfig() attention.MultiHeadConfig {
	return m.config
}
