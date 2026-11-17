package simd

import "testing"

func TestGuardPage_Luma8EdgeDispatch(t *testing.T) {
	if !guardChild(t) {
		return
	}
	for _, dir := range []string{"vertical", "horizontal"} {
		t.Run(dir, func(t *testing.T) {
			w, h := 16, 7 // 水平最后一行是q2，q3不在切片内。
			if dir == "vertical" {
				w, h = 8, 16
			}
			got, stride := guardedRows(t, w, h, 1)
			want := make([]byte, w*h)
			for y := range h {
				for x := range w {
					v := byte(100 + y*2 + x*2)
					got[y*stride+x], want[y*w+x] = v, v
				}
			}
			bs, tc := [4]uint8{4, 1, 0, 3}, [4]int{0, 2, 3, 4}
			q, along, across := 4*stride, 1, stride
			sq, sa, sc := 4*w, 1, w
			if dir == "vertical" {
				q, along, across = 4, stride, 1
				sq, sa, sc = 4, w, 1
			}
			filterLuma8EdgeGeneric(want, sq, sa, sc, bs, 40, 20, tc)
			FilterLuma8Edge(got, q, along, across, bs, 40, 20, tc)
			for y := range h {
				for x := range w {
					if got[y*stride+x] != want[y*w+x] {
						t.Fatalf("(%d,%d)结果不符", x, y)
					}
				}
			}
		})
	}
}
