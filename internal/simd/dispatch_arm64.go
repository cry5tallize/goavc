//go:build arm64 && !purego

package simd

func platformKernels(cpu cpuFeatures, table kernelTable) kernelTable {
	if cpu.neon {
		table.sad8, table.sad16 = sad8NEON, sad16NEON
		table.satd4 = satd4NEON
		table.backends = Backends{SAD8: NEON, SAD16: NEON, SATD4x4: NEON}
	}
	return table
}
