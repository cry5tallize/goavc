//go:build amd64 && !purego

package simd

// 输入/输出各32字节，元素低32位有效，不要求16/32字节对齐。
func quantMultiplySSE2(a, b [4]uint64) (out [4]uint64)
func quantMultiplyAVX2(a, b [4]uint64) (out [4]uint64)
func installQuantKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.quantMultiply, t.backends.QuantMultiply = quantMultiplySSE2, SSE2
	}
	if cpu.sse2 && cpu.avx2 {
		t.quantMultiply, t.backends.QuantMultiply = quantMultiplyAVX2, AVX2
	}
	return t
}
