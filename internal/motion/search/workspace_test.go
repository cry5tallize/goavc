package search

import (
	"fmt"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
)

func assertWorkspaceUnbound(t *testing.T, w *Workspace) {
	t.Helper()
	if w.bytes.ref != nil || w.words.ref != nil || w.bytes.reference != nil || w.words.reference != nil {
		t.Fatal("搜索结束仍借用参考图像")
	}
}

func TestAllocation_PreparedSearch(t *testing.T) {
	// 同一个调用者工作区跨位深、几何与场复用；禁止把新工作区分配移到每次计时外。
	w := new(Workspace)
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		source, _ := picture.New(3, 4, format)
		reference, _ := picture.New(3, 4, format)
		for y := range 64 {
			for x := range 48 {
				reference.Planes[0].SetSample(x, y, uint16((x*x*29+y*y*47+x*y*13)&((1<<format.BitDepth())-1)))
			}
		}
		for _, field := range []bool{false, true} {
			src, ref := source, reference
			if field {
				a, _ := picture.FieldView(source, true)
				b, _ := picture.FieldView(reference, true)
				src, ref = &a, &b
			}
			for _, edge := range []bool{false, true} {
				x, y := 13, 7
				if edge {
					x, y = 0, 0
				}
				for _, full := range []bool{false, true} {
					for _, entry := range []string{"Integer", "Refine", "Estimate", "EstimateWithCost"} {
						name := fmt.Sprintf("%d/field=%v/edge=%v/full=%v/%s", format.BitDepth(), field, edge, full, entry)
						t.Run(name, func(t *testing.T) {
							// 构造可精确找到的真实非空块，单独检查MV/SAD，避免仅调用空路径。
							want := MV{X: -1, Y: 2}
							if entry == "Integer" {
								want = MV{X: -4, Y: 4}
							}
							p, r := &src.Planes[0], &ref.Planes[0]
							if format.BitDepth() == 8 {
								prediction.MCBlockLuma(p.Data[p.Index(x, y):], p.Stride, r, x, y, 7, 13, int(want.X), int(want.Y))
							} else {
								prediction.MCBlockLumaHigh(p.Data16[p.Index(x, y):], p.Stride, r, x, y, 7, 13, int(want.X), int(want.Y))
							}
							var mv MV
							var sad, calls int
							run := func() {
								switch entry {
								case "Integer":
									mv, sad = Integer(p, r, x, y, 7, 13, want, 3, full, w)
								case "Refine":
									mv, sad = Refine(p, r, x, y, 7, 13, MV{}, 3, w)
								case "Estimate":
									mv, sad = Estimate(p, r, x, y, 7, 13, want, 3, full, w)
								default:
									// 闭包在每次真实搜索处建立；不能只测已预先在堆上准备的Scorer。
									mv, sad = EstimateWithCost(p, r, x, y, 7, 13, MV{}, 3, full, func(m MV, sad int) uint64 {
										calls++
										return uint64(sad) + uint64(absTrace(int(m.X-want.X))+absTrace(int(m.Y-want.Y)))*(1<<24)
									}, w)
								}
							}
							allocs := testing.AllocsPerRun(20, run)
							if allocs != 0 {
								t.Fatalf("prepared %s: %g allocs", entry, allocs)
							}
							// 无代价菱形是局部搜索；只要求结果可重复，不伪称一定找到全局最优。
							beforeMV, beforeSAD := mv, sad
							run()
							if mv != beforeMV || sad != beforeSAD {
								t.Fatal("复用改变搜索结果")
							}
							if entry == "EstimateWithCost" && (mv != want || sad != 0 || calls == 0) {
								t.Fatalf("want %v/0, got %v/%d, calls=%d", want, mv, sad, calls)
							}
							assertWorkspaceUnbound(t, w)
						})
					}
				}
			}
		}
	}
}

func TestWorkspace_PanicUnbindsBorrow(t *testing.T) {
	src, _ := picture.New(1, 1, h264.YUV420P)
	var w Workspace
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Scorer panic未传播")
			}
		}()
		EstimateWithCost(&src.Planes[0], &src.Planes[0], 0, 0, 7, 13, MV{}, 2, false, func(MV, int) uint64 { panic("scorer") }, &w)
	}()
	assertWorkspaceUnbound(t, &w)
	mv, sad := Integer(&src.Planes[0], &src.Planes[0], 0, 0, 16, 16, MV{}, 2, false, &w)
	if mv != (MV{}) || sad != 0 {
		t.Fatal(mv, sad)
	}
}
