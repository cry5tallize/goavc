//go:build amd64 && !purego

package simd

import "testing"

func TestParity_SSE2SAD(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU不能执行SSE2，未执行")
	}
	checkSADParity(t, selectKernels(cpuFeatures{sse2: true}))
}

func TestParity_AVX2SAD(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("当前CPU/OS不能执行AVX2，未执行")
	}
	checkSADParity(t, selectKernels(cpuFeatures{sse2: true, avx2: true}))
}

func BenchmarkSAD8_16x16_AVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("当前CPU/OS不能执行AVX2，未执行")
	}
	benchmarkSAD8(b, sad8AVX2)
}

func BenchmarkSAD16_16x16_AVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("当前CPU/OS不能执行AVX2，未执行")
	}
	benchmarkSAD16(b, sad16AVX2)
}

func TestDispatch_MissingCPUUsesGeneric(t *testing.T) {
	table := selectKernels(cpuFeatures{})
	if table.backends != (Backends{}) {
		t.Fatalf("缺能力错误选择%+v", table.backends)
	}
	checkSADParity(t, table)
}

func BenchmarkSAD8_16x16_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("当前CPU不能执行SSE2，未执行")
	}
	benchmarkSAD8(b, sad8SSE2)
}

func BenchmarkSAD16_16x16_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("当前CPU不能执行SSE2，未执行")
	}
	benchmarkSAD16(b, sad16SSE2)
}
