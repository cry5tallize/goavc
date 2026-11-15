package simd

import "testing"

func TestFilter_LocalAllocation(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		var low [32]byte
		var high [32]uint16
		for i := range low {
			low[i] = 100
			high[i] = 515
		}
		for _, strength := range []int{1, 4} {
			FilterLuma8(low[:], 4, 8, 1, strength, 40, 20, 2, 4, 8)
			FilterLuma16(high[:], 4, 8, 1, strength, 160, 80, 8, 4, 10)
			FilterChroma8(low[:], 4, 8, 1, strength, 40, 20, 3, 4, 8)
			FilterChroma16(high[:], 4, 8, 1, strength, 160, 80, 9, 4, 10)
		}
		for i, v := range high {
			if v != 515 || low[i] != 100 {
				panic("常量滤波或低两位改变")
			}
		}
	}); n != 0 {
		t.Fatalf("领域局部去块缓冲分配=%v，期望0", n)
	}
}
