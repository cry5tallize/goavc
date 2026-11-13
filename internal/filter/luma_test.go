package filter

import "testing"

// edgeRows 构造 rows 行相同图案的缓冲，stride=8。
func edgeRows(rows int, p0, p1, p2, p3, q0, q1, q2, q3 byte) ([]byte, int) {
	const stride = 8
	pix := make([]byte, stride*rows)
	for y := 0; y < rows; y++ {
		copy(pix[y*stride:y*stride+8], []byte{p3, p2, p1, p0, q0, q1, q2, q3})
	}
	return pix, stride
}

func TestLumaEdge_ZeroBS(t *testing.T) {
	pix, stride := edgeRows(4, 10, 10, 10, 10, 20, 20, 20, 20)
	orig := append([]byte(nil), pix...)
	LumaEdge(pix, 4, stride, 1, 0, 30, 0, 0)
	for i := range pix {
		if pix[i] != orig[i] {
			t.Fatalf("bS=0 modified pixel %d: %d != %d", i, pix[i], orig[i])
		}
	}
}

func TestLumaEdge_StrongKnownValues(t *testing.T) {
	pix, stride := edgeRows(4, 10, 10, 10, 10, 20, 20, 20, 20)
	// q0 在每行索引 4；qp=51 -> alpha=255,beta=18。
	LumaEdge(pix, 4, stride, 1, 4, 51, 0, 0)
	want := []byte{11, 13, 14, 16, 18, 19} // p2..q2
	for y := 0; y < 4; y++ {
		got := pix[y*stride+1 : y*stride+7]
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("row %d filtered p2..q2 = %v, want %v", y, got, want)
			}
		}
	}
}

func TestLumaEdge_WeakKnownValues(t *testing.T) {
	pix, stride := edgeRows(4, 10, 10, 10, 10, 20, 20, 20, 20)
	// qp=51,bS=1: tc0Table[103][1]=13 -> delta=4。
	LumaEdge(pix, 4, stride, 1, 1, 51, 0, 0)
	for y := 0; y < 4; y++ {
		if p, q := pix[y*stride+3], pix[y*stride+4]; p != 14 || q != 16 {
			t.Fatalf("row %d weak filtered p0/q0 = %d/%d, want 14/16", y, p, q)
		}
	}
}

func TestLumaEdge_ConstantUnaffected(t *testing.T) {
	for _, bS := range []int{1, 2, 3, 4} {
		pix, stride := edgeRows(4, 50, 50, 50, 50, 50, 50, 50, 50)
		LumaEdge(pix, 4, stride, 1, bS, 51, 0, 0)
		for i, v := range pix {
			if v != 50 {
				t.Fatalf("bS=%d modified constant pixel %d to %d", bS, i, v)
			}
		}
	}
}

func TestLumaEdge_WeakUsesOriginalNeighbors(t *testing.T) {
	pix, stride := edgeRows(4, 10, 0, 10, 10, 20, 21, 20, 20)
	LumaEdge(pix, 4, stride, 1, 1, 51, 0, 0)
	// Spec: 8.7.2.3。delta=(4*(20-10)+0-21+4)>>3=2。
	// p1/q1 同时会被更新，但不能用更新值重算 delta。
	for y := 0; y < 4; y++ {
		if pix[y*stride+3] != 12 || pix[y*stride+4] != 18 {
			t.Fatalf("row %d: %v", y, pix[y*stride:y*stride+8])
		}
	}
}

func TestLumaEdge_HorizontalOrientation(t *testing.T) {
	// 水平边界：q0 在 (0,y)，p0 在上一行；lineStep=1、acrossStep=stride。
	const stride = 8
	pix := make([]byte, stride*8)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if y < 4 {
				pix[y*stride+x] = 10
			} else {
				pix[y*stride+x] = 20
			}
		}
	}
	LumaEdge(pix, 4*stride, 1, stride, 1, 51, 0, 0)
	if pix[3*stride] != 14 || pix[4*stride] != 16 {
		t.Fatalf("horizontal weak p0/q0 = %d/%d, want 14/16", pix[3*stride], pix[4*stride])
	}
}
