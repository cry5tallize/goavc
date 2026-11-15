//go:build arm64 && !purego

package simd

import "testing"

func TestParity_NEONSATD(t *testing.T) {
	if !nativeCPU.neon {
		t.Skip("当前平台不能执行NEON，未执行")
	}
	checkSATDParity(t, satd4NEON)
}

func BenchmarkSATD_4x4_NEON(b *testing.B) {
	if !nativeCPU.neon {
		b.Skip("当前平台不能执行NEON，未执行")
	}
	benchmarkSATD(b, satd4NEON)
}
