package attention

import (
	takara "github.com/takara-ai/go-attention/attention"
)

type Engine struct {
	model *takara.MultiHeadAttention
}

func NewEngine() *Engine {
	cfg := takara.MultiHeadConfig{
		NumHeads: 4,
		DModel:   64,
		DKey:     16,
		DValue:   16,
	}

	m, _ := takara.NewMultiHeadAttention(cfg)

	return &Engine{model: m}
}
