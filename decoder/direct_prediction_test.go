package decoder

import (
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/syntax"
)

func TestDirectPrediction_CoverageAndPixels(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV422P, h264.YUV444P, h264.YUV420P10LE, h264.YUV422P10LE, h264.YUV444P10LE} {
		for _, size := range []int{2, 4} {
			for pattern := range 4 {
				got, want := deblockTestState(t, 2, 2, format, true), deblockTestState(t, 2, 2, format, true)
				r0, r1 := deblockTestState(t, 2, 2, format, true), deblockTestState(t, 2, 2, format, true)
				s := sliceEntry{pps: &syntax.PPS{WeightedBipredIDC: 2}, hdr: &syntax.SliceHeader{}}
				s.refs[0] = []*dpb.Reference{{Picture: r0.pic, Identity: &dpb.Identity{POC: -2}}}
				s.refs[1] = []*dpb.Reference{{Picture: r1.pic, Identity: &dpb.Identity{POC: 3}}}
				var original [16]bPart
				for y := range size {
					for x := range size {
						mv := motion.MV{X: 3, Y: -5}
						switch pattern {
						case 1:
							mv.X += int32(x/2 + y/2*2)
						case 2:
							mv.X += int32(y)
						case 3:
							mv.X += int32(x + y*size)
						}
						original[y*size+x] = bPart{pPartition: pPartition{x, y, 1, 1}, refs: [2]int8{0, 0}, mvs: [2]motion.MV{mv, {X: -1, Y: 7}}}
					}
				}
				parts := original
				n := coalesceDirectPrediction(parts[:size*size], size, size)
				if pattern == 0 && n != 1 || pattern == 3 && n != size*size {
					t.Fatalf("分区数量错误：size=%d pattern=%d n=%d", size, pattern, n)
				}
				var covered [16]bool
				for _, p := range parts[:n] {
					for y := p.y; y < p.y+p.h; y++ {
						for x := p.x; x < p.x+p.w; x++ {
							i := y*size + x
							if covered[i] || original[i].mvs != p.mvs || original[i].refs != p.refs {
								t.Fatal("合并任务重叠或跨越不同运动信息")
							}
							covered[i] = true
						}
					}
					got.mcBPartition(&s, 1, 1, p.pPartition, p.refs, p.mvs)
				}
				for i, p := range original[:size*size] {
					if !covered[i] {
						t.Fatal("合并任务遗漏像素区域")
					}
					want.mcBPartition(&s, 1, 1, p.pPartition, p.refs, p.mvs)
				}
				for c := range 3 {
					if !slices.Equal(got.pic.Planes[c].Data, want.pic.Planes[c].Data) || !slices.Equal(got.pic.Planes[c].Data16, want.pic.Planes[c].Data16) {
						t.Fatalf("合并预测像素不一致：%s size=%d pattern=%d plane=%d", format, size, pattern, c)
					}
				}
			}
		}
	}
}
