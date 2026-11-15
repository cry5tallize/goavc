package simd

import (
	"math/rand"
	"testing"
)

func checkSATDParity(t *testing.T, fn satd4Func) {
	t.Helper()
	for _, value := range []int32{-1023, -255, -3, 0, 3, 255, 1023} {
		for _, impulse := range []bool{false, true} {
			var src [16]int32
			if impulse {
				src[7] = value
			} else {
				for i := range src {
					src[i] = value
				}
			}
			// 常量只产生DC，冲激产生16个等幅系数；解析SATD均为8*abs(value)。
			if got, want := fn(src), 8*int(max(value, -value)); got != want {
				t.Fatalf("value=%d impulse=%t SATD=%d，期望%d", value, impulse, got, want)
			}
		}
	}
	const seed = 0x84a7
	rng := rand.New(rand.NewSource(seed))
	for n := range 5000 {
		var src [16]int32
		for i := range src {
			src[i] = int32(rng.Intn(2047) - 1023)
		}
		if got, want := fn(src), satd4Generic(src); got != want {
			t.Fatalf("seed=%d case=%d src=%v SATD=%d，期望%d", seed, n, src, got, want)
		}
	}
}

func TestParity_SATD(t *testing.T) {
	t.Run("Generic", func(t *testing.T) { checkSATDParity(t, satd4Generic) })
	t.Run("Auto", func(t *testing.T) { checkSATDParity(t, SATD4x4) })
}

func TestAllocation_SATD(t *testing.T) {
	if got := testing.AllocsPerRun(1000, func() {
		var src [16]int32
		for i := range src {
			src[i] = 3
		}
		if SATD4x4(src) != 24 {
			panic("SATD常量解析值改变")
		}
	}); got != 0 {
		t.Fatalf("SATD生产包装分配=%v，期望0", got)
	}
}

func benchmarkSATD(b *testing.B, fn satd4Func) {
	var src [16]int32
	for i := range src {
		src[i] = int32(i*137%2047) - 1023
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBenchmarkResult = fn(src)
	}
}

func BenchmarkSATD_4x4_Generic(b *testing.B) { benchmarkSATD(b, satd4Generic) }
func BenchmarkSATD_4x4_Auto(b *testing.B)    { benchmarkSATD(b, SATD4x4) }
