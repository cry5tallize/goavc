package simd

import "testing"

func TestResidualDC_ExtremesStrideAndAllocations(t *testing.T) {
	for _, dc := range []int32{-2147483648, -1024, -1, 0, 1, 1024, 2147483647} {
		for _, w := range []int{1, 4, 7} {
			var a, b [128]byte
			var u, v [128]uint16
			var residual [64]int32
			for i := range a {
				a[i], b[i] = byte(i*7), byte(i*7)
				u[i], v[i] = uint16(i*13%1024), uint16(i*13%1024)
			}
			for i := range residual {
				residual[i] = dc
			}
			stride, h := w+3, 3
			n := (h-1)*stride + w
			AddResidualDC8(a[:n], stride, w, h, dc)
			AddResidual8(b[:n], stride, residual[:], w, w, h)
			AddResidualDC16(u[:n], stride, w, h, dc)
			AddResidual16(v[:n], stride, residual[:], w, w, h)
			if a != b || u != v {
				t.Fatalf("常量残差不一致：dc=%d width=%d", dc, w)
			}
		}
	}
	if n := testing.AllocsPerRun(100, func() { var data [16]uint16; AddResidualDC16(data[:], 4, 4, 4, -33) }); n != 0 {
		t.Fatalf("常量残差发生分配：%v", n)
	}
}
