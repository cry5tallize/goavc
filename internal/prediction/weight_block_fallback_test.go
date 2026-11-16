package prediction

import "testing"

func TestWeightBlock_WideParametersAndAliases(t *testing.T) {
	for _, shape := range [][4]int{{0, 1, 19, 19}, {1, 0, 19, 19}, {0, 0, 19, 19}, {0, 0, 23, 19}} {
		for _, c := range []struct {
			w, o  [2]int32
			denom uint32
			depth int
		}{
			{[2]int32{-128, 127}, [2]int32{-128, 127}, 0, 10},
			{[2]int32{-128, 127}, [2]int32{-128, 127}, 0, 8},
			{[2]int32{1 << 30, -1 << 30}, [2]int32{1 << 30, -1 << 30}, 7, 10},
			{[2]int32{127, -128}, [2]int32{-3, 4}, 9, 9},
			{[2]int32{-3, 7}, [2]int32{2, -3}, 63, 10},
		} {
			for _, both := range []bool{false, true} {
				var got, want, other [512]byte
				var high, expected, second [512]uint16
				for i := range got {
					got[i] = byte(i * 71)
					high[i] = uint16(i * 131)
					other[i] = byte(255 - i*23)
					second[i] = uint16(65535 - i*73)
				}
				want, expected = got, high
				d, s, ds, ss := shape[0], shape[1], shape[2], shape[3]
				for y := range 5 {
					for x := range 8 {
						i, j := d+y*ds+x, s+y*ss+x
						if both {
							want[i] = BiWeightSample(want[j], other[y*ss+x], c.w[0], c.w[1], c.o[0], c.o[1], c.denom)
							expected[i] = BiWeightSampleHigh(expected[j], second[y*ss+x], c.w[0], c.w[1], c.o[0], c.o[1], c.denom, c.depth)
						} else {
							want[i] = WeightSample(want[j], c.w[0], c.o[0], c.denom)
							expected[i] = WeightSampleHigh(expected[j], c.w[0], c.o[0], c.denom, c.depth)
						}
					}
				}
				if both {
					BiWeightBlock8(got[d:], ds, got[s:], other[:], ss, 8, 5, c.w, c.o, c.denom)
					BiWeightBlock16(high[d:], ds, high[s:], second[:], ss, 8, 5, c.w, c.o, c.denom, c.depth)
				} else {
					WeightBlock8(got[d:], ds, got[s:], ss, 8, 5, c.w[0], c.o[0], c.denom)
					WeightBlock16(high[d:], ds, high[s:], ss, 8, 5, c.w[0], c.o[0], c.denom, c.depth)
				}
				if got != want || high != expected {
					t.Fatalf("别名/宽域回退不符：shape%v config%+v both%t", shape, c, both)
				}
			}
		}
	}
	if n := testing.AllocsPerRun(100, func() {
		var a, b, d [256]byte
		var u, v, out [256]uint16
		WeightBlock8(d[:], 16, a[:], 16, 16, 16, -32, 127, 5)
		BiWeightBlock8(d[:], 16, a[:], b[:], 16, 16, 16, [2]int32{-64, 127}, [2]int32{-128, 127}, 6)
		WeightBlock16(out[:], 16, u[:], 16, 16, 16, -32, 127, 5, 10)
		BiWeightBlock16(out[:], 16, u[:], v[:], 16, 16, 16, [2]int32{-64, 127}, [2]int32{-128, 127}, 6, 10)
	}); n != 0 {
		t.Fatalf("局部块实际加权分配%g次", n)
	}
}
