package decoder

import (
	"fmt"
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
	"github.com/cry5tallize/goavc/syntax"
)

func biMCFixture(t testing.TB, format h264.PixelFormat) (*frameState, *sliceEntry) {
	pic, err := picture.New(4, 4, format)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pic.Release)
	sw, sh := format.ChromaSubsampling()
	cat := uint32(1)
	if sw == 1 {
		cat = 3
	} else if sh == 1 {
		cat = 2
	}
	fs := &frameState{pic: pic, subW: sw, subH: sh, sps: &syntax.SPS{ChromaFormatIDC: cat, FrameMbsOnlyFlag: true}, poc: 2}
	s := &sliceEntry{sps: fs.sps, pps: &syntax.PPS{}, hdr: &syntax.SliceHeader{}}
	for l := range 2 {
		r, err := picture.New(4, 4, format)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(r.Release)
		for c := range 3 {
			p := &r.Planes[c]
			for y := range p.Height {
				for x := range p.Width {
					p.SetSample(x, y, uint16((x*71+y*37+c*53+l*97)%(1<<p.BitDepth)))
				}
			}
		}
		s.refs[l] = []*dpb.Reference{{Picture: r, Identity: &dpb.Identity{ID: uint64(l + 1), POC: int32(l * 4)}}}
	}
	return fs, s
}

// 原两份预测块加第三遍均值，作为融合调度的完整缓冲对照。
func biMCReference(fs *frameState, s *sliceEntry, mx, my int, p pPartition, mvs [2]motion.MV) {
	for c := range 3 {
		x, y, w, h := mx*16+p.x*4, my*16+p.y*4, p.w*4, p.h*4
		if c > 0 {
			x /= fs.subW
			y /= fs.subH
			w /= fs.subW
			h /= fs.subH
		}
		pl := &fs.pic.Planes[c]
		if pl.BitDepth == 8 {
			var a, b [256]byte
			mcBPlane(fs, a[:], w, s.refs[0][0], c, x, y, w, h, mvs[0])
			mcBPlane(fs, b[:], w, s.refs[1][0], c, x, y, w, h, mvs[1])
			prediction.BiWeightBlock8(pl.Data[pl.Index(x, y):], pl.Stride, a[:], b[:], w, w, h, [2]int32{1, 1}, [2]int32{}, 0)
		} else {
			var a, b [256]uint16
			mcBPlane(fs, a[:], w, s.refs[0][0], c, x, y, w, h, mvs[0])
			mcBPlane(fs, b[:], w, s.refs[1][0], c, x, y, w, h, mvs[1])
			prediction.BiWeightBlock16(pl.Data16[pl.Index(x, y):], pl.Stride, a[:], b[:], w, w, h, [2]int32{1, 1}, [2]int32{}, 0, pl.BitDepth)
		}
	}
}

func TestBiMC_PutAverageParity(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV444P} {
		fs, s := biMCFixture(t, format)
		want, _ := biMCFixture(t, format)
		for fy := range 4 {
			for fx := range 4 {
				for _, xy := range [][2]int{{0, 0}, {1, 1}, {3, 3}} {
					for _, size := range []int{1, 2, 4} {
						p := pPartition{w: size, h: size}
						mvs := [2]motion.MV{{X: int32(fx - 4), Y: int32(fy - 4)}, {X: int32(3 - fx), Y: int32(3 - fy)}}
						for c := range 3 {
							clear(fs.pic.Planes[c].Data)
							clear(fs.pic.Planes[c].Data16)
							clear(want.pic.Planes[c].Data)
							clear(want.pic.Planes[c].Data16)
						}
						fs.mcBPartition(s, xy[0], xy[1], p, [2]int8{0, 0}, mvs)
						biMCReference(want, s, xy[0], xy[1], p, mvs)
						for c := range 3 {
							if !slices.Equal(fs.pic.Planes[c].Data, want.pic.Planes[c].Data) || !slices.Equal(fs.pic.Planes[c].Data16, want.pic.Planes[c].Data16) {
								t.Fatalf("%s phase%d,%d xy%v size%d plane%d不符", format, fx, fy, xy, size, c)
							}
						}
					}
				}
			}
		}
	}
}

func BenchmarkBiMC_16_Auto(b *testing.B) {
	for _, c := range []struct {
		name string
		mv   [2]motion.MV
		xy   int
	}{{"integer", [2]motion.MV{}, 1}, {"qpel", [2]motion.MV{{X: 1, Y: 2}, {X: 3, Y: 1}}, 1}, {"boundary", [2]motion.MV{{X: -23, Y: -27}, {X: 3, Y: 1}}, 0}} {
		b.Run(fmt.Sprint(c.name), func(b *testing.B) {
			fs, s := biMCFixture(b, h264.YUV420P)
			b.ReportAllocs()
			for b.Loop() {
				fs.mcBPartition(s, c.xy, c.xy, pPartition{w: 4, h: 4}, [2]int8{0, 0}, c.mv)
			}
		})
	}
}
