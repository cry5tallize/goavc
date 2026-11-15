//go:build arm64 && !purego

package simd

import "testing"

func TestParity_NEONSAD(t *testing.T) {
	if !nativeCPU.neon {
		t.Skip("当前平台不能执行NEON，未执行")
	}
	checkSADParity(t, selectKernels(cpuFeatures{neon: true}))
}

func BenchmarkSAD8_16x16_NEON(b *testing.B) {
	if !nativeCPU.neon {
		b.Skip("当前平台不能执行NEON，未执行")
	}
	benchmarkSAD8(b, sad8NEON)
}

func BenchmarkSAD16_16x16_NEON(b *testing.B) {
	if !nativeCPU.neon {
		b.Skip("当前平台不能执行NEON，未执行")
	}
	benchmarkSAD16(b, sad16NEON)
}
