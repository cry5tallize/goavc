package transform

import "testing"

func TestAllocation_SATDLocalResidual(t *testing.T) {
	if got := testing.AllocsPerRun(1000, func() {
		var src [16]int32
		for i := range src {
			src[i] = 3
		}
		if SATD4x4(&src) != 24 {
			panic("SATD低两位解析值改变")
		}
	}); got != 0 {
		t.Fatalf("领域局部残差逃逸，分配=%v", got)
	}
}

func TestSATD_AnalyticBasisAndStride(t *testing.T) {
	for _, v := range []int32{-255, -1, 1, 255} {
		var residual [16]int32
		for i := range residual {
			residual[i] = v
		}
		if got := SATD4x4(&residual); got != int(max(v, -v))*8 {
			t.Fatal(v, got)
		}
		clear(residual[:])
		residual[5] = v
		if got := SATD4x4(&residual); got != int(max(v, -v))*8 {
			t.Fatal(v, got)
		}
	}
	for _, size := range []int{8, 16} {
		a, b := make([]byte, (size+3)*size), make([]byte, (size+5)*size)
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				a[y*(size+3)+x] = 100
				b[y*(size+5)+x] = 99
			}
		}
		if got := SATD(a, size+3, b, size+5, size, size); got != size*size/2 {
			t.Fatal(size, got)
		}
	}
}
