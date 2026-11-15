//go:build amd64 && !purego

package simd

func residualSSE2(a, b [8]int32) [8]int32
func residualAVX2(a, b [8]int32) [8]int32

func installResidualKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.residualVector, t.backends.Residual = residualSSE2, SSE2
	}
	if cpu.sse2 && cpu.avx2 {
		t.residualVector, t.backends.Residual = residualAVX2, AVX2
	}
	return t
}
