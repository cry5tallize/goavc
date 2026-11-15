//go:build arm64 && !purego

package simd

func quantMultiplyNEON(a, b [4]uint64) (out [4]uint64)
func installQuantKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.neon {
		t.quantMultiply, t.backends.QuantMultiply = quantMultiplyNEON, NEON
	}
	return t
}
