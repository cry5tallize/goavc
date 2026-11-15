package simd

import (
	"math/rand"
	"testing"
)

func checkForward(t *testing.T, fn forward4Func) {
	t.Helper()
	basis := [4][4]int32{{1, 1, 1, 1}, {2, 1, -1, -2}, {1, -1, -1, 1}, {1, -2, 2, -1}}
	// 独立矩阵乘法解析解，覆盖正负冲激、10位低两位和最大合法值。
	for _, v := range []int32{-1023, -255, -3, 1, 3, 255, 1023} {
		for pos := range 16 {
			var src, want [16]int32
			src[pos] = v
			for y := range 4 {
				for x := range 4 {
					want[y*4+x] = v * basis[y][pos/4] * basis[x][pos%4]
				}
			}
			if got := fn(src); got != want {
				t.Fatalf("impulse %d/%d: got %v want %v", pos, v, got, want)
			}
		}
	}
	rng := rand.New(rand.NewSource(0x1044))
	for n := range 5000 {
		var src [16]int32
		for i := range src {
			src[i] = int32(rng.Intn(2047) - 1023)
			if n%7 == 0 {
				src[i] = int32(rng.Uint32())
			}
		}
		if got, want := fn(src), forward4Generic(src); got != want {
			t.Fatalf("sample %d: got %v want %v", n, got, want)
		}
	}
}

func TestForward_GenericAndAuto(t *testing.T) {
	checkForward(t, forward4Generic)
	checkForward(t, Forward4x4)
	if n := testing.AllocsPerRun(100, func() {
		var src [16]int32
		src[3] = 1023
		if Forward4x4(src)[0] != 1023 {
			panic("forward")
		}
	}); n != 0 {
		t.Fatal(n)
	}
	t.Logf("Forward4x4=%s", ActiveBackends().Forward4x4)
}

func benchForward(b *testing.B, fn forward4Func) {
	var src, out [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	for range b.N {
		out = fn(src)
	}
	if out[0] == 0 {
		b.Fatal("empty transform")
	}
}
func BenchmarkForward4x4_4x4_Generic(b *testing.B) { benchForward(b, forward4Generic) }
func BenchmarkForward4x4_4x4_Auto(b *testing.B)    { benchForward(b, Forward4x4) }
