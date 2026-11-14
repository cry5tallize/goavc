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
		got, cost := Estimate(p, &ref.Planes[0], 16, 16, 16, 16, want, 4, true)
		if got != want || cost != 0 {
			t.Fatalf("want %v got %v cost %d", want, got, cost)
		}
	}
}
func TestSearch_DiamondAndBounds(t *testing.T) {
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
		_, full := Estimate(p, &ref.Planes[0], x, y, 16, 16, MV{}, 8, true)
		mv, diamond := Estimate(p, &ref.Planes[0], x, y, 16, 16, MV{}, 8, false)
		// 菱形是局部搜索；本解析斜坡中允许每像素平均至多 1 的 SAD 差值，
		// 不把它错误宣称为与穷举全局最优等价。
		if diamond > full+256 || mv.X < -32 || mv.X > 32 || mv.Y < -32 || mv.Y > 32 {
			t.Fatal(mv, diamond, full)
		}
	}
}

func TestSearch_CostCallbackPreservesReportedSAD(t *testing.T) {
	ref, _ := picture.New(2, 2, h264.YUV420P)
	src, _ := picture.New(2, 2, h264.YUV420P)
	// 常量块所有位移的 SAD 都为 0；上层代价可以选择零之外的 MVP。
	want := MV{X: 8, Y: 4}
	got, sad := EstimateWithCost(&src.Planes[0], &ref.Planes[0], 8, 8, 8, 8, want, 4, true, func(m MV, sad int) uint64 {
		dx, dy := int(m.X-want.X), int(m.Y-want.Y)
		return uint64(sad + max(dx, -dx) + max(dy, -dy))
	})
	if got != want || sad != 0 {
		t.Fatal(got, sad)
	}
}
