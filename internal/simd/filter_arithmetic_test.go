package simd

import "testing"

func checkFilterArithmetic(t *testing.T, fn func([4]int32, [4]int32) [4]int32) {
	t.Helper()
	values := [...]int32{-8192, -1023, -1, 0, 1, 1023, 8192}
	for _, a := range values {
		for _, b := range values {
			x := [4]int32{a, b, -a, -b}
			y := [4]int32{b, -a, a, -b}
			got, want := fn(x, y), filterDeltaGeneric(x, y)
			if got != want {
				t.Fatalf("去块候选向量不一致：输入 %v %v，得到 %v，期望 %v", x, y, got, want)
			}
		}
	}
	if n := testing.AllocsPerRun(100, func() { fn([4]int32{1, 2, 3, 4}, [4]int32{4, 3, 2, 1}) }); n != 0 {
		t.Fatalf("去块候选计算发生分配：%v", n)
	}
}

func checkFilterEdges(t *testing.T, fn func([4]int32, [4]int32) [4]int32) {
	t.Helper()
	for _, depth := range []int{8, 10} {
		for _, bs := range []int{1, 2, 3, 4} {
			for _, span := range []int{1, 2, 4} {
				for _, steps := range [][2]int{{16, 1}, {1, 16}, {2, 16}} {
					for seed := range 16 {
						var a, b [160]uint16
						base := 40 + seed*3
						for i := range a {
							a[i] = uint16((base + (i*7+seed)%17) << (depth - 8))
						}
						b = a
						q, line, across := 64, steps[0], steps[1]
						if depth == 8 {
							var u, v [160]byte
							for i := range u {
								u[i] = byte(a[i])
							}
							v = u
							filterLuma(u[:], q, line, across, bs, 80, 40, 4, span, depth, filterDeltaGeneric)
							filterLuma(v[:], q, line, across, bs, 80, 40, 4, span, depth, fn)
							if u != v {
								t.Fatalf("8位亮度去块不一致：强度%d、跨度%d、步长%v", bs, span, steps)
							}
							filterChroma(u[:], q, line, across, bs, 80, 40, 5, span, depth, filterDeltaGeneric)
							filterChroma(v[:], q, line, across, bs, 80, 40, 5, span, depth, fn)
							if u != v {
								t.Fatalf("8位色度去块不一致：强度%d、跨度%d、步长%v", bs, span, steps)
							}
						}
						filterLuma(a[:], q, line, across, bs, 80, 40, 4, span, depth, filterDeltaGeneric)
						filterLuma(b[:], q, line, across, bs, 80, 40, 4, span, depth, fn)
						if a != b {
							t.Fatalf("亮度去块不一致：位深%d、强度%d、跨度%d、步长%v、种子%d", depth, bs, span, steps, seed)
						}
						filterChroma(a[:], q, line, across, bs, 80, 40, 5, span, depth, filterDeltaGeneric)
						filterChroma(b[:], q, line, across, bs, 80, 40, 5, span, depth, fn)
						if a != b {
							t.Fatalf("色度去块不一致：位深%d、强度%d、跨度%d、步长%v、种子%d", depth, bs, span, steps, seed)
						}
					}
				}
			}
		}
	}
}

func BenchmarkFilterArithmeticGeneric(b *testing.B) {
	x, y := [4]int32{1, 2, 3, 4}, [4]int32{5, 6, 7, 8}
	for b.Loop() {
		filterDeltaGeneric(x, y)
	}
}
