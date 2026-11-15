//go:build amd64 && !purego

package simd

func platformKernels(cpu cpuFeatures, table kernelTable) kernelTable {
	if cpu.sse2 {
		table.sad8, table.sad16 = sad8SSE2, sad16SSE2
		table.satd4 = satd4SSE2
		table.backends = Backends{SAD8: SSE2, SAD16: SSE2, SATD4x4: SSE2}
	}
	if cpu.sse2 && cpu.avx2 {
		table.sad8, table.sad16 = sad8AVX2, sad16AVX2
		table.satd4 = satd4AVX2
		table.backends = Backends{SAD8: AVX2, SAD16: AVX2, SATD4x4: AVX2}
	}
	return table
}
