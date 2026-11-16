package prediction

import "testing"

func TestWeightBlock_SampleParityAndBounds(t *testing.T) {
	for _, depth := range []int{8, 10} {
		for _, size := range [][2]int{{4, 4}, {7, 5}, {8, 16}, {16, 16}} {
			for _, c := range []struct {
				w, o [2]int32
				d    uint32
			}{
				{[2]int32{1, 1}, [2]int32{}, 0},
				{[2]int32{32, 32}, [2]int32{}, 5},
				{[2]int32{1, 3}, [2]int32{-3, 4}, 2},
				{[2]int32{-64, 127}, [2]int32{-128, 127}, 6},
				{[2]int32{128, -128}, [2]int32{127, -128}, 7},
			} {
				var a, b [19 * 17]uint16
				var u, v [19 * 17]byte
				for i := range a {
					a[i], b[i] = uint16(i*71%(1<<depth)), uint16((i*37+3)%(1<<depth))
					u[i], v[i] = byte(a[i]), byte(b[i])
				}
				for _, both := range []bool{false, true} {
					var got, want [23 * 17]uint16
					var low, expected [23 * 17]byte
					for i := range got {
						got[i], want[i], low[i], expected[i] = 77, 77, 77, 77
					}
					w, h := size[0], size[1]
					if depth == 8 {
						if both {
							BiWeightBlock8(low[1:], 23, u[1:], v[1:], 19, w, h, c.w, c.o, c.d)
						} else {
							WeightBlock8(low[1:], 23, u[1:], 19, w, h, c.w[0], c.o[0], c.d)
						}
					} else if both {
						BiWeightBlock16(got[1:], 23, a[1:], b[1:], 19, w, h, c.w, c.o, c.d, depth)
					} else {
						WeightBlock16(got[1:], 23, a[1:], 19, w, h, c.w[0], c.o[0], c.d, depth)
					}
					for y := range h {
						for x := range w {
							i, j := y*23+x+1, y*19+x+1
							if depth == 8 {
								if both {
									expected[i] = BiWeightSample(u[j], v[j], c.w[0], c.w[1], c.o[0], c.o[1], c.d)
								} else {
									expected[i] = WeightSample(u[j], c.w[0], c.o[0], c.d)
								}
							} else if both {
								want[i] = BiWeightSampleHigh(a[j], b[j], c.w[0], c.w[1], c.o[0], c.o[1], c.d, depth)
							} else {
								want[i] = WeightSampleHigh(a[j], c.w[0], c.o[0], c.d, depth)
							}
						}
					}
					if got != want || low != expected {
						t.Fatalf("块/单样本不一致或写到矩形外：depth=%d size=%v both=%v config=%+v", depth, size, both, c)
					}
				}
			}
		}
	}
}

func TestWeightBlock_LocalAllocation(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		var a, b, d [256]byte
		BiWeightBlock8(d[:], 16, a[:], b[:], 16, 16, 16, [2]int32{1, 1}, [2]int32{}, 0)
		WeightBlock8(d[:], 16, a[:], 16, 16, 16, 1, 0, 0)
	}); n != 0 {
		t.Fatalf("局部块加权分配=%v", n)
	}
}
