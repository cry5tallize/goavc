package filter

import (
	"bytes"
	"testing"
)

func TestChroma420Edge_FallbackAndAllocations(t *testing.T) {
	for _, g := range []struct{ q, along, across, size int }{
		{2, 19, 1, 137}, {38, 1, 19, 65}, // 最小len、非对齐stride。
		{2, 19, 1, 136}, {38, 1, 19, 64}, // 末线q1不足，精确跳过该线。
		{2, 4, 1, 32}, {2, 2, 2, 24}, // 未进入SIMD的短stride和非标准步长。
	} {
		for _, depth := range []int{8, 10} {
			got := make([]byte, g.size)
			for i := range got {
				got[i] = byte(96 + i%5)
			}
			want := bytes.Clone(got)
			p := PrepareEdge(32, 0, 0, depth)
			bs := [4]uint8{0, 4, 1, 3}
			p.Chroma4208(got, g.q, g.along, g.across, bs)
			p.Chroma8(want, g.q, g.along, g.across, bs, 2)
			if !bytes.Equal(got, want) {
				t.Fatalf("逐段fallback不符：g=%+v depth=%d", g, depth)
			}
		}
	}
	for _, g := range [][3]int{{2, 19, 1}, {38, 1, 19}, {2, 4, 1}} {
		p := PrepareEdge(32, 0, 0, 8)
		if n := testing.AllocsPerRun(100, func() {
			// 数组必须在closure内，避免只测预先分配的堆缓冲。
			var local [192]byte
			for i := range local {
				local[i] = byte(96 + i%5)
			}
			p.Chroma4208(local[:], g[0], g[1], g[2], [4]uint8{0, 4, 1, 3})
		}); n != 0 {
			t.Fatalf("g=%v分配%g次，期望0", g, n)
		}
	}
	PrepareEdge(0, 0, 0, 8).Chroma4208(nil, -1, 0, 0, [4]uint8{4, 3, 2, 1})
	PrepareEdge(32, 0, 0, 8).Chroma4208(nil, -1, 0, 0, [4]uint8{})
	maxInt := int(^uint(0) >> 1)
	for _, g := range [][3]int{{-1, 19, 1}, {2, maxInt, 1}, {maxInt, 19, 1}, {2, 19, maxInt}, {2, 0, 1}} {
		if chroma420EdgeHasCore(192, g[0], g[1], g[2]) {
			t.Fatalf("邻域检查接受溢出或无效几何：%v", g)
		}
	}
}
