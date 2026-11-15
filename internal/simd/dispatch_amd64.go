//go:build amd64 && !purego

package simd

func platformKernels(cpu cpuFeatures, table kernelTable) kernelTable {
	if cpu.sse2 {
		table.sad8, table.sad16 = sad8SSE2, sad16SSE2
		table.backends = Backends{SAD8: SSE2, SAD16: SSE2}
	}
	return table
}
