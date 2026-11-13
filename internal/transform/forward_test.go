package transform

import "testing"

func TestForward4x4_AnalyticBasis(t *testing.T) {
	basis := [4][4]int32{{1, 1, 1, 1}, {2, 1, -1, -2}, {1, -1, -1, 1}, {1, -2, 2, -1}}
	for pos := 0; pos < 16; pos++ {
		var src, dst [16]int32
		src[pos] = 7
		Forward4x4(&dst, &src)
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				want := 7 * basis[y][pos/4] * basis[x][pos%4]
				if dst[y*4+x] != want {
					t.Fatalf("basis %d (%d,%d): %d want %d", pos, x, y, dst[y*4+x], want)
				}
			}
		}
	}
}
func TestForwardDC_AnalyticBasis(t *testing.T) {
	var src, dst [16]int32
	for i := range src {
		src[i] = 16
	}
	ForwardLumaDC(&dst, &src)
	if dst[0] != 128 {
		t.Fatal(dst)
	}
	for _, v := range dst[1:] {
		if v != 0 {
			t.Fatal(dst)
		}
	}
	c := [4]int32{16, 16, 16, 16}
	var d [4]int32
	ForwardChromaDC(&d, &c)
	if d != [4]int32{64, 0, 0, 0} {
		t.Fatal(d)
	}
}
func TestQuantization_ConstantResidualAndQPEdges(t *testing.T) {
	for qp := 0; qp <= 51; qp++ {
		var src, coeff, levels, deq, res [16]int32
		for i := range src {
			src[i] = 40
		}
		Forward4x4(&coeff, &src)
		if err := Quant4x4(&levels, &coeff, qp); err != nil {
			t.Fatal(err)
		}
		if err := Dequant4x4(&deq, &levels, qp); err != nil {
			t.Fatal(err)
		}
		IDCT4x4(&res, &deq)
		for _, v := range res {
			if v < 0 || v > 80 {
				t.Fatalf("qp=%d residual %d", qp, v)
			}
		}
		// 常量 DC 独立解：QP=0 时 640*13107/32768 舍入为 256，反量化 2560，残差40。
		if qp == 0 && (levels[0] != 256 || res[0] != 40) {
			t.Fatal(levels, res)
		}
	}
}
