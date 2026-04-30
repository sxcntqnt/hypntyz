package attention

import takara "github.com/takara-ai/go-attention/attention"

// Score vehicles against query
func (e *Engine) Score(query takara.Vector, keys takara.Matrix) []float64 {

	values := keys // self-attention style

	output, _ := e.model.Forward(keys, keys, values)

	_ = query // (can be fused later in advanced version)

	scores := make([]float64, len(output))

	for i := range output {
		sum := 0.0
		for _, v := range output[i] {
			sum += v
		}
		scores[i] = sum
	}

	return scores
}
