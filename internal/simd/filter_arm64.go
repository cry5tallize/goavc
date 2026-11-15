//go:build arm64 && !purego

package simd

func filterDeltaNEON(a, b [4]int32) (out [4]int32)

func installFilterKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.neon {
		t.filterDelta, t.backends.FilterDelta = filterDeltaNEON, NEON
	}
	return t
}
