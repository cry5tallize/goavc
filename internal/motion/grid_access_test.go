package motion

import "testing"

func availableReference(g *Grid, bx, by int) bool {
	if g == nil || bx < 0 || by < 0 || bx >= g.BlockW || by >= g.BlockH {
		return false
	}
	mb := by/4*g.MBWidth + bx/4
	if g.mappedOwners != nil {
		return mb < len(g.mappedOwners) && g.mappedOwners[mb] == g.mappedSlice && g.valid[g.index(bx, by)]
	}
	return mb >= g.sliceStart && g.valid[g.index(bx, by)]
}

func TestGridAccess_SliceAndMappedParity(t *testing.T) {
	g := NewGrid(5, 3)
	for i := range g.valid {
		g.valid[i] = i%3 != 0
		g.MV[i] = MV{X: int32(i), Y: -int32(i)}
		g.Ref[i] = int8(i % 4)
	}
	for _, start := range []int{-1, 0, 1, 7, 15} {
		for _, owners := range [][]int{nil, {}, {0, 1, 0}, {0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0}} {
			g.BeginSlice(start)
			if owners != nil {
				g.BeginMappedSlice(owners, 1)
			}
			for y := -1; y <= g.BlockH; y++ {
				for x := -1; x <= g.BlockW; x++ {
					want := availableReference(g, x, y)
					mv, ref, ok := g.Get(x, y)
					if ok != want || g.Available(x, y) != want {
						t.Fatal("可用性或slice权限改变")
					}
					if !want && (mv != (MV{}) || ref != 0) {
						t.Fatal("不可用块泄露运动信息")
					}
					if want && (mv != g.MV[g.index(x, y)] || ref != g.Ref[g.index(x, y)]) {
						t.Fatal("可用块数据改变")
					}
				}
			}
		}
	}
	var absent *Grid
	if _, _, ok := absent.Get(0, 0); ok || absent.Available(0, 0) {
		t.Fatal("nil网格被视为可用")
	}
}

var gridAccessSink int

func BenchmarkGridGet_1080p_Generic(b *testing.B) {
	g := NewGrid(120, 68)
	g.SetRect(0, 0, g.BlockW, g.BlockH, MV{X: 1, Y: -2}, 3)
	x, y, sum := 0, 0, 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		mv, ref, ok := g.Get(x, y)
		if !ok {
			b.Fatal("基准网格不可用")
		}
		sum += int(mv.X) + int(ref)
		x++
		if x == g.BlockW {
			x = 0
			y++
			if y == g.BlockH {
				y = 0
			}
		}
	}
	gridAccessSink = sum
}
