package features

import "sync"

type VectorPool struct {
	pool sync.Pool
}

func NewVectorPool(size int) *VectorPool {
	return &VectorPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]float64, size)
			},
		},
	}
}

func (vp *VectorPool) Get() []float64 {
	return vp.pool.Get().([]float64)
}

func (vp *VectorPool) Put(v []float64) {
	vp.pool.Put(v)
}

type MatrixPool struct {
	pool sync.Pool
}

func NewMatrixPool(rows, cols int) *MatrixPool {
	return &MatrixPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([][]float64, rows)
			},
		},
	}
}

func (mp *MatrixPool) Get() [][]float64 {
	return mp.pool.Get().([][]float64)
}

func (mp *MatrixPool) Put(m [][]float64) {
	mp.pool.Put(m)
}
