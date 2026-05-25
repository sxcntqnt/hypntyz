package memory

import (
	"math"

	"hypnotz/internal/types"
)

type EmbeddingEngine struct {
	dim int
}

func NewEmbeddingEngine(dim int) *EmbeddingEngine {
	return &EmbeddingEngine{dim: dim}
}

func (ee *EmbeddingEngine) Encode(entity *MemoryEntity) types.Vector {
	embedding := make(types.Vector, ee.dim)

	if ee.dim >= 1 {
		embedding[0] = entity.Position.Lat
	}
	if ee.dim >= 2 {
		embedding[1] = entity.Position.Lon
	}
	if ee.dim >= 3 {
		embedding[2] = entity.Velocity.Speed / 120.0
	}
	if ee.dim >= 4 {
		embedding[3] = math.Sin(entity.Velocity.Heading)
	}
	if ee.dim >= 5 {
		embedding[4] = math.Cos(entity.Velocity.Heading)
	}
	if ee.dim >= 6 {
		embedding[5] = entity.Salience
	}
	if ee.dim >= 7 {
		embedding[6] = entity.RiskScore
	}

	return embedding
}

func (ee *EmbeddingEngine) UpdateEmbedding(entity *MemoryEntity, event types.VehicleState) {
	alpha := 0.95
	beta := 0.05

	if len(entity.Embedding) == 0 {
		entity.Embedding = ee.Encode(entity)
		return
	}

	currentEmbedding := ee.Encode(entity)

	for i := range entity.Embedding {
		if i < len(currentEmbedding) {
			entity.Embedding[i] = alpha*entity.Embedding[i] + beta*currentEmbedding[i]
		}
	}

	entity.Embedding = entity.Embedding[:ee.dim]
}

func (ee *EmbeddingEngine) Similarity(a, b types.Vector) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	dot := 0.0
	normA := 0.0
	normB := 0.0

	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func (ee *EmbeddingEngine) Distance(a, b types.Vector) float64 {
	if len(a) != len(b) {
		return math.MaxFloat64
	}

	sum := 0.0
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return math.Sqrt(sum)
}

func (ee *EmbeddingEngine) Nearest(target types.Vector, entities []*MemoryEntity) *MemoryEntity {
	if len(entities) == 0 {
		return nil
	}

	var nearest *MemoryEntity
	minDist := math.MaxFloat64

	for _, entity := range entities {
		embedding := ee.Encode(entity)
		dist := ee.Distance(target, embedding)
		if dist < minDist {
			minDist = dist
			nearest = entity
		}
	}

	return nearest
}

func (ee *EmbeddingEngine) Cluster(entities []*MemoryEntity, k int) [][]*MemoryEntity {
	if len(entities) == 0 || k <= 0 {
		return [][]*MemoryEntity{}
	}

	if k > len(entities) {
		k = len(entities)
	}

	clusters := make([][]*MemoryEntity, k)
	for i := range clusters {
		clusters[i] = make([]*MemoryEntity, 0)
	}

	for _, entity := range entities {
		minDist := math.MaxFloat64
		clusterIdx := 0

		for i := 0; i < k; i++ {
			if i < len(clusters) && len(clusters[i]) > 0 {
				center := clusters[i][0]
				dist := ee.Distance(ee.Encode(center), ee.Encode(entity))
				if dist < minDist {
					minDist = dist
					clusterIdx = i
				}
			}
		}

		clusters[clusterIdx] = append(clusters[clusterIdx], entity)
	}

	return clusters
}
