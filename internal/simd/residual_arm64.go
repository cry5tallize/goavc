//go:build arm64 && !purego

package simd

func residualNEON(a, b [8]int32) [8]int32
func installResidualKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.neon {
		t.residualVector, t.backends.Residual = residualNEON, NEON
	}
	return t
}
