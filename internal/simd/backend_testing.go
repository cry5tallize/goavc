//go:build goavc_simdtest

package simd

import (
	"errors"
	"sync"
)

// ErrBackendUnavailable 表示指定的测试后端未实现或当前CPU不能安全执行。
var ErrBackendUnavailable = errors.New("simd：测试后端不可用")

var backendTestMu sync.Mutex

// ForceBackendForTesting 仅存在于测试标签。所有调用者须串行使用，且切换/恢复
// 前join全部worker；不能与t.Parallel或并发benchmark混用。生产构建不含此入口。
func ForceBackendForTesting(backend Backend) (restore func(), err error) {
	backendTestMu.Lock()
	table, err := testBackendTable(backend)
	if err != nil {
		backendTestMu.Unlock()
		return nil, err
	}
	previous := kernels
	kernels = table
	var once sync.Once
	return func() { once.Do(func() { kernels = previous; backendTestMu.Unlock() }) }, nil
}

// ForceLuma8EdgeBackendForTesting 仅替换亮度整边槽；调用方须串行使用并先join worker。
func ForceLuma8EdgeBackendForTesting(backend Backend) (restore func(), err error) {
	backendTestMu.Lock()
	previous := kernels
	table := previous
	switch backend {
	case Generic:
		table.filterLuma8Edge, table.backends.FilterLumaEdge = filterLuma8EdgeGeneric, Generic
	case SSE2:
		if !nativeCPU.sse2 {
			backendTestMu.Unlock()
			return nil, ErrBackendUnavailable
		}
		sse := selectKernels(cpuFeatures{sse2: true})
		if sse.backends.FilterLumaEdge != SSE2 {
			backendTestMu.Unlock()
			return nil, ErrBackendUnavailable
		}
		table.filterLuma8Edge, table.backends.FilterLumaEdge = sse.filterLuma8Edge, SSE2
	default:
		backendTestMu.Unlock()
		return nil, ErrBackendUnavailable
	}
	kernels = table
	var once sync.Once
	return func() { once.Do(func() { kernels = previous; backendTestMu.Unlock() }) }, nil
}

// ForceChroma8EdgeBackendForTesting 仅替换色度整边槽，调用方必须串行使用。
func ForceChroma8EdgeBackendForTesting(backend Backend) (restore func(), err error) {
	backendTestMu.Lock()
	if backend != Generic && (backend != SSE2 || !nativeCPU.sse2 || selectKernels(nativeCPU).backends.FilterChromaEdge != SSE2) {
		backendTestMu.Unlock()
		return nil, ErrBackendUnavailable
	}
	previous := kernels.backends.FilterChromaEdge
	kernels.backends.FilterChromaEdge = backend
	var once sync.Once
	return func() { once.Do(func() { kernels.backends.FilterChromaEdge = previous; backendTestMu.Unlock() }) }, nil
}

func testBackendTable(backend Backend) (kernelTable, error) {
	if backend == Generic {
		return genericTable(), nil
	}
	if backend == SSE2 && nativeCPU.sse2 {
		table := selectKernels(cpuFeatures{sse2: true})
		if table.backends.SAD8 == SSE2 && table.backends.SAD16 == SSE2 && table.backends.FilterLumaEdge == SSE2 {
			return table, nil
		}
	}
	if backend == AVX2 && nativeCPU.avx2 {
		table := selectKernels(cpuFeatures{sse2: true, avx2: true})
		if table.backends.SAD8 == AVX2 && table.backends.SAD16 == AVX2 {
			return table, nil
		}
	}
	if backend == NEON && nativeCPU.neon {
		table := selectKernels(cpuFeatures{neon: true})
		if table.backends.SAD8 == NEON && table.backends.SAD16 == NEON {
			return table, nil
		}
	}
	// 只有交付真实计算后端后才能扩展；CPU支持不能代替实现。
	return kernelTable{}, ErrBackendUnavailable
}
