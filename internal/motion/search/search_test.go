package search

import (
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
	"testing"
)

func TestSAD_ExtremesAndUnalignedStride(t *testing.T) {
	for _, size := range [][2]int{{16, 16}, {8, 8}, {7, 13}} {
		w, h := size[0], size[1]
		a, b := make([]byte, (w+3)*h), make([]byte, (w+5)*h)
		want := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				a[y*(w+3)+x] = byte(x + y*7)
				b[y*(w+5)+x] = 255
				want += 255 - int(a[y*(w+3)+x])
			}
		}
		if got := SAD(a, w+3, b, w+5, w, h); got != want {
			t.Fatal(size, got, want)
		}
	}
}

func TestSearch_TranslationAndSubpixel(t *testing.T) {
	var workspace Workspace
	ref, _ := picture.New(4, 4, h264.YUV420P)
	src, _ := picture.New(4, 4, h264.YUV420P)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			ref.Planes[0].SetPix(x, y, byte((x*x+3*y*y+x*y)%251))
		}
	}
	for _, want := range []MV{{}, {X: 12, Y: -8}, {X: -7, Y: 5}, {X: 2, Y: -1}} {
		p := &src.Planes[0]
		prediction.MCBlockLuma(p.Data[p.Index(16, 16):], p.Stride, &ref.Planes[0], 16, 16, 16, 16, int(want.X), int(want.Y))
		got, cost := Estimate(p, &ref.Planes[0], 16, 16, 16, 16, want, 4, true, &workspace)
		if got != want || cost != 0 {
			t.Fatalf("want %v got %v cost %d", want, got, cost)
		}
	}
}
func TestSearch_DiamondAndBounds(t *testing.T) {
	var workspace Workspace
	ref, _ := picture.New(2, 2, h264.YUV420P)
	src, _ := picture.New(2, 2, h264.YUV420P)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			ref.Planes[0].SetPix(x, y, byte(x*4+y*3))
		}
	}
	for _, xy := range [][2]int{{0, 0}, {16, 16}} {
		x, y := xy[0], xy[1]
		p := &src.Planes[0]
		prediction.MCBlockLuma(p.Data[p.Index(x, y):], p.Stride, &ref.Planes[0], x, y, 16, 16, 8, -4)
		_, full := Estimate(p, &ref.Planes[0], x, y, 16, 16, MV{}, 8, true, &workspace)
		mv, diamond := Estimate(p, &ref.Planes[0], x, y, 16, 16, MV{}, 8, false, &workspace)
		// 菱形是局部搜索；本解析斜坡中允许每像素平均至多 1 的 SAD 差值，
		// 不把它错误宣称为与穷举全局最优等价。
		if diamond > full+256 || mv.X < -32 || mv.X > 32 || mv.Y < -32 || mv.Y > 32 {
			t.Fatal(mv, diamond, full)
		}
	}
}

func TestSearch_CostCallbackPreservesReportedSAD(t *testing.T) {
	var workspace Workspace
	ref, _ := picture.New(2, 2, h264.YUV420P)
	src, _ := picture.New(2, 2, h264.YUV420P)
	// 常量块所有位移的 SAD 都为 0；上层代价可以选择零之外的 MVP。
	want := MV{X: 8, Y: 4}
	got, sad := EstimateWithCost(&src.Planes[0], &ref.Planes[0], 8, 8, 8, 8, want, 4, true, func(m MV, sad int) uint64 {
		dx, dy := int(m.X-want.X), int(m.Y-want.Y)
		return uint64(sad + max(dx, -dx) + max(dy, -dy))
	}, &workspace)
	if got != want || sad != 0 {
		t.Fatal(got, sad)
	}
}

func highSearchPlane() picture.Plane {
	return picture.Plane{Data16: make([]uint16, 35*32), Width: 32, Height: 32, Stride: 35, BitDepth: 10}
}

func TestSearchHigh_IntegerTranslationLowBits(t *testing.T) {
	var workspace Workspace
	for _, lowBits := range []bool{false, true} {
		ref, src := highSearchPlane(), highSearchPlane()
		state := uint32(1)
		for y := range ref.Height {
			for x := range ref.Width {
				state = state*1664525 + 1013904223
				mask := uint32(511)
				if lowBits {
					mask = 3
				}
				ref.Data16[ref.Index(x, y)] = 512 + uint16((state>>16)&mask)
			}
		}
		for _, tc := range []struct {
			x, y int
			mv   MV
		}{{8, 8, MV{X: -8, Y: 4}}, {0, 0, MV{X: -4, Y: -8}}, {24, 24, MV{X: 8, Y: 4}}} {
			for y := range 8 {
				for x := range 8 {
					rx := min(31, max(0, tc.x+x+int(tc.mv.X)/4))
					ry := min(31, max(0, tc.y+y+int(tc.mv.Y)/4))
					src.Data16[src.Index(tc.x+x, tc.y+y)] = ref.Data16[ref.Index(rx, ry)]
				}
			}
			for _, full := range []bool{false, true} {
				got, sad := Integer(&src, &ref, tc.x, tc.y, 8, 8, tc.mv, 4, full, &workspace)
				if got != tc.mv || sad != 0 {
					t.Fatalf("lowBits=%v full=%v: want %v, got %v SAD=%d", lowBits, full, tc.mv, got, sad)
				}
			}
			got, sad := Estimate(&src, &ref, tc.x, tc.y, 8, 8, tc.mv, 4, true, &workspace)
			if got != tc.mv || sad != 0 {
				t.Fatalf("lowBits=%v: want %v, got %v SAD=%d", lowBits, tc.mv, got, sad)
			}
		}
	}
}

func TestSearchHigh_AnalyticSubpixel(t *testing.T) {
	var workspace Workspace
	ref, src := highSearchPlane(), highSearchPlane()
	// 线性平面的六抽头插值等于解析值，四分之一像素也恰好为整数。
	for y := range 32 {
		for x := range 32 {
			ref.Data16[ref.Index(x, y)] = uint16(200 + 8*x + 12*y)
		}
	}
	for _, want := range []MV{{X: -1, Y: 2}, {X: 1, Y: -2}, {X: 2, Y: 1}, {X: -2, Y: -1}} {
		for y := 8; y < 16; y++ {
			for x := 8; x < 16; x++ {
				src.Data16[src.Index(x, y)] = uint16(200 + 8*x + 12*y + 2*int(want.X) + 3*int(want.Y))
			}
		}
		// 解析斜坡可能有等价位移；用位移代价固定目标，独立验证原位深 SAD。
		scorer := func(m MV, sad int) uint64 {
			dx, dy := int(m.X-want.X), int(m.Y-want.Y)
			return uint64(sad + 65536*(max(dx, -dx)+max(dy, -dy)))
		}
		got, sad := EstimateWithCost(&src, &ref, 8, 8, 8, 8, MV{}, 4, true, scorer, &workspace)
		if got != want || sad != 0 {
			t.Fatalf("want %v, got %v SAD=%d", want, got, sad)
		}
		_, sad = Refine(&src, &ref, 8, 8, 8, 8, MV{}, 4, &workspace)
		if sad != 0 {
			t.Fatalf("want zero refined SAD for %v, got %d", want, sad)
		}
	}
}

func TestSearchHigh_ScorerReceivesFullRangeSAD(t *testing.T) {
	var workspace Workspace
	for _, size := range [][2]int{{16, 16}, {7, 13}} {
		ref, src := highSearchPlane(), highSearchPlane()
		for i := range src.Data16 {
			src.Data16[i] = 1023
		}
		want := 1023 * size[0] * size[1]
		calls := 0
		got, sad := EstimateWithCost(&src, &ref, 0, 0, size[0], size[1], MV{}, 4, true, func(m MV, sad int) uint64 {
			calls++
			if sad != want {
				t.Fatalf("MV=%v: want full-range SAD %d, got %d", m, want, sad)
			}
			return uint64(sad)
		}, &workspace)
		if got != (MV{}) || sad != want || calls == 0 {
			t.Fatal(got, sad, calls)
		}
	}
}

func TestSearchHigh_SubpixelBounds(t *testing.T) {
	var workspace Workspace
	ref, src := highSearchPlane(), highSearchPlane()
	for y := range ref.Height {
		for x := range ref.Width {
			ref.Data16[ref.Index(x, y)] = uint16((x*x*29 + y*y*47 + x*y*13) & 1023)
		}
	}
	for _, tc := range []struct {
		x, y int
		mv   MV
	}{{0, 0, MV{X: -1, Y: -2}}, {25, 19, MV{X: 2, Y: 1}}} {
		prediction.MCBlockLumaHigh(src.Data16[src.Index(tc.x, tc.y):], src.Stride, &ref, tc.x, tc.y, 7, 13, int(tc.mv.X), int(tc.mv.Y))
		got, sad := EstimateWithCost(&src, &ref, tc.x, tc.y, 7, 13, MV{}, 4, true, func(m MV, sad int) uint64 {
			dx, dy := int(m.X-tc.mv.X), int(m.Y-tc.mv.Y)
			return uint64(sad + 65536*(max(dx, -dx)+max(dy, -dy)))
		}, &workspace)
		if got != tc.mv || sad != 0 {
			t.Fatalf("at (%d,%d): want %v, got %v SAD=%d", tc.x, tc.y, tc.mv, got, sad)
		}
	}
}
