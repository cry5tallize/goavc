//go:build amd64 && !purego

package simd

import "testing"

func TestParity_SSE2SATD(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU不能执行SSE2，未执行")
	}
	checkSATDParity(t, satd4SSE2)
}

func TestParity_AVX2SATD(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("当前CPU/OS不能执行AVX2，未执行")
	}
	checkSATDParity(t, satd4AVX2)
}

func BenchmarkSATD_4x4_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("当前CPU不能执行SSE2，未执行")
	}
	benchmarkSATD(b, satd4SSE2)
}

func BenchmarkSATD_4x4_AVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("当前CPU/OS不能执行AVX2，未执行")
	}
	benchmarkSATD(b, satd4AVX2)
}
