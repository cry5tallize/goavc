//go:build amd64 && !purego

package simd

func filterDeltaSSE2(a, b [4]int32) (out [4]int32)

// filterLuma8EdgeSSE2要求调用方保证四段的p2至q2邻域有效。
// p3、q3仅在强滤波条件成立且邻点存在时读取。
//
//go:noescape
func filterLuma8EdgeSSE2(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int)

func filterDeltaAVX2(a, b [4]int32) (out [4]int32)

func installFilterKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.filterLuma8Edge, t.backends.FilterLumaEdge = filterLuma8EdgeSSE2, SSE2
		t.backends.FilterChromaEdge = SSE2
	}
	if cpu.sse2 {
		t.filterDelta, t.backends.FilterDelta = filterDeltaSSE2, SSE2
	}
	if cpu.sse2 && cpu.avx2 {
		t.filterDelta, t.backends.FilterDelta = filterDeltaAVX2, AVX2
	}
	return t
}
