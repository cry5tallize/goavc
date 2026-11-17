//go:build amd64 && !purego

package simd

func filterDeltaSSE2(a, b [4]int32) (out [4]int32)

// filterLuma8EdgeSSE2要求调用方保证四段的p2至q2邻域有效。
// p3、q3仅在强滤波条件成立且邻点存在时读取。
//
//go:noescape
func filterLuma8EdgeSSE2(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int)

// 具名调用让编译器保留汇编的 noescape 契约；后端槽仍由初始化或串行测试选择。
func filterLuma8EdgeNative(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int) {
	if TryFilterLuma8Edge(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc) {
		return
	}
	filterLuma8EdgeGeneric(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc)
}

// TryFilterLuma8Edge 执行当前启用的整边 SIMD。调用方先验证完整邻域；
// false 时未访问像素，仍可按原逐段策略处理。
func TryFilterLuma8Edge(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int) bool {
	if kernels.backends.FilterLumaEdge != SSE2 {
		return false
	}
	filterLuma8EdgeSSE2(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc)
	return true
}

func filterDeltaAVX2(a, b [4]int32) (out [4]int32)

func installFilterKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.backends.FilterLumaEdge = SSE2
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
