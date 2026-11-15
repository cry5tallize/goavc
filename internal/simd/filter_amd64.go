//go:build amd64 && !purego

package simd

func filterDeltaSSE2(a, b [4]int32) (out [4]int32)
func filterDeltaAVX2(a, b [4]int32) (out [4]int32)

func installFilterKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.filterDelta, t.backends.FilterDelta = filterDeltaSSE2, SSE2
	}
	if cpu.sse2 && cpu.avx2 {
		t.filterDelta, t.backends.FilterDelta = filterDeltaAVX2, AVX2
	}
	return t
}
