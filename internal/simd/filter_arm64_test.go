//go:build arm64 && !purego

package simd

import "testing"

func TestFilterArithmeticNEON(t *testing.T) {
	if !nativeCPU.neon {
		t.Skip("当前CPU不能执行NEON")
	}
	checkFilterArithmetic(t, filterDeltaNEON)
	checkFilterEdges(t, filterDeltaNEON)
}

func BenchmarkFilterArithmeticNEON(b *testing.B) {
	if !nativeCPU.neon {
		b.Skip("当前CPU不能执行NEON")
	}
	x, y := [4]int32{1, 2, 3, 4}, [4]int32{5, 6, 7, 8}
	for b.Loop() {
		filterDeltaNEON(x, y)
	}
}
