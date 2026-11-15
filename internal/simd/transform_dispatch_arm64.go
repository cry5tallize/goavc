//go:build arm64 && !purego

package simd

func installTransformKernels(cpu cpuFeatures, table kernelTable) kernelTable {
	if cpu.neon {
		table.forward4, table.backends.Forward4x4 = forward4NEON, NEON
		table.forward8, table.backends.Forward8x8 = forward8NEON, NEON
		table.idct4, table.backends.IDCT4x4 = idct4NEON, NEON
		table.idct8, table.backends.IDCT8x8 = idct8NEON, NEON
		table.forwardLumaDC, table.backends.ForwardLumaDC = forwardLumaDCNEON, NEON
		table.forwardChromaDC, table.backends.ForwardChromaDC = forwardChromaDCNEON, NEON
		table.forwardChromaDC422, table.backends.ForwardChromaDC422 = forwardChromaDC422NEON, NEON
		table.wideAdd4, table.backends.WideAdd4 = wideAdd4NEON, NEON
	}
	return table
}
