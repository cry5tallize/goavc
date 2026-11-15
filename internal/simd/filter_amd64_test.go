//go:build amd64 && !purego

package simd

import "testing"

func TestFilterArithmeticSSE2(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU不能执行SSE2")
	}
	checkFilterArithmetic(t, filterDeltaSSE2)
	checkFilterEdges(t, filterDeltaSSE2)
}

func TestFilterArithmeticAVX2(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("当前CPU或系统不能执行AVX2")
	}
	checkFilterArithmetic(t, filterDeltaAVX2)
	checkFilterEdges(t, filterDeltaAVX2)
}

func BenchmarkFilterArithmeticSSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("当前CPU不能执行SSE2")
	}
	x, y := [4]int32{1, 2, 3, 4}, [4]int32{5, 6, 7, 8}
	for b.Loop() {
		filterDeltaSSE2(x, y)
	}
}

func BenchmarkFilterArithmeticAVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("当前CPU或系统不能执行AVX2")
	}
	x, y := [4]int32{1, 2, 3, 4}, [4]int32{5, 6, 7, 8}
	for b.Loop() {
		filterDeltaAVX2(x, y)
	}
}
