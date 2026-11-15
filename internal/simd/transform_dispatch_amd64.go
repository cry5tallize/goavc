//go:build amd64 && !purego

package simd

func installTransformKernels(cpu cpuFeatures, table kernelTable) kernelTable {
	if cpu.sse2 {
		table.forward4, table.backends.Forward4x4 = forward4SSE2, SSE2
		table.forward8, table.backends.Forward8x8 = forward8SSE2, SSE2
		table.idct4, table.backends.IDCT4x4 = idct4SSE2, SSE2
		table.idct8, table.backends.IDCT8x8 = idct8SSE2, SSE2
		table.forwardLumaDC, table.backends.ForwardLumaDC = forwardLumaDCSSE2, SSE2
		table.forwardChromaDC, table.backends.ForwardChromaDC = forwardChromaDCSSE2, SSE2
		table.forwardChromaDC422, table.backends.ForwardChromaDC422 = forwardChromaDC422SSE2, SSE2
		table.wideAdd4, table.backends.WideAdd4 = wideAdd4SSE2, SSE2
	}
	if cpu.sse2 && cpu.avx2 {
		table.forward4, table.backends.Forward4x4 = forward4AVX2, AVX2
		table.forward8, table.backends.Forward8x8 = forward8AVX2, AVX2
		table.idct4, table.backends.IDCT4x4 = idct4AVX2, AVX2
		table.idct8, table.backends.IDCT8x8 = idct8AVX2, AVX2
		table.forwardLumaDC, table.backends.ForwardLumaDC = forwardLumaDCAVX2, AVX2
		table.forwardChromaDC, table.backends.ForwardChromaDC = forwardChromaDCAVX2, AVX2
		table.forwardChromaDC422, table.backends.ForwardChromaDC422 = forwardChromaDC422AVX2, AVX2
		table.wideAdd4, table.backends.WideAdd4 = wideAdd4AVX2, AVX2
	}
	return table
}
