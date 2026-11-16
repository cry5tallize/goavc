package decoder

import (
	"fmt"
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
	"github.com/cry5tallize/goavc/syntax"
)

func pWeightFixture(t testing.TB, format h264.PixelFormat, separate, field bool, component, mode int, ld, cd uint32) (*frameState, *sliceEntry) {
	t.Helper()
	pic, err := picture.New(1, 1, format)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pic.Release)
	sw, sh := format.ChromaSubsampling()
	fs := &frameState{pic: pic, subW: sw, subH: sh, field: field, component: component,
		sps: &syntax.SPS{SeparateColourPlaneFlag: separate}}
	w := syntax.PredictionWeight{Weight: [3]int32{1 << ld, 1 << cd, 1 << cd}}
	if mode&1 != 0 {
		w.Weight[0], w.Offset[0] = -32, 127
	}
	if mode&2 != 0 {
		w.Weight[1], w.Offset[1], w.Weight[2], w.Offset[2] = 5, -4, -7, 5
	}
	table := &syntax.WeightTable{LumaDenom: ld, ChromaDenom: cd}
	table.Lists[0] = []syntax.PredictionWeight{{Weight: [3]int32{1, 1, 1}, Offset: [3]int32{99, 99, 99}}, w}
	s := &sliceEntry{pps: &syntax.PPS{WeightedPredFlag: true}, hdr: &syntax.SliceHeader{PredictionWeights: table}}
	for c := range pic.Planes {
		p := &pic.Planes[c]
		for y := range p.Height {
			for x := range p.Width {
				p.SetSample(x, y, uint16((x*71+y*37+c*13)%(1<<p.BitDepth)))
			}
		}
	}
	return fs, s
}

// 原逐样本公式用于核对分量、denom、场ref转换和独立平面索引；
// 规范算术及FFmpeg Oracle继续由共享预测与codec测试覆盖。
func pWeightReference(fs *frameState, s *sliceEntry, x, y, w, h, ref int) {
	if !s.pps.WeightedPredFlag || s.hdr.PredictionWeights == nil {
		return
	}
	t := s.hdr.PredictionWeights
	if fs.field {
		ref /= 2
	}
	weight := t.Lists[0][ref]
	for c := range 3 {
		if fs.sps.SeparateColourPlaneFlag && c != fs.component {
			continue
		}
		wi, px, py, pw, ph, d := c, x, y, w, h, t.LumaDenom
		if fs.sps.SeparateColourPlaneFlag {
			wi = 0
		} else if c > 0 {
			px /= fs.subW
			py /= fs.subH
			pw /= fs.subW
			ph /= fs.subH
			d = t.ChromaDenom
		}
		p := &fs.pic.Planes[c]
		for yy := range ph {
			for xx := range pw {
				v := p.Sample(px+xx, py+yy)
				p.SetSample(px+xx, py+yy, prediction.WeightSampleHigh(v, weight.Weight[wi], weight.Offset[wi], d, p.BitDepth))
			}
		}
	}
}

func TestPWeights_ComponentIdentityAndSampleParity(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV422P, h264.YUV444P, h264.YUV420P10LE, h264.YUV422P10LE, h264.YUV444P10LE} {
		for _, field := range []bool{false, true} {
			for mode := range 4 {
				for _, denoms := range [][2]uint32{{5, 2}, {0, 7}} {
					separates := []int{-1}
					if format == h264.YUV444P || format == h264.YUV444P10LE {
						separates = []int{-1, 0, 1, 2}
					}
					for _, component := range separates {
						t.Run(fmt.Sprintf("%s/field%t/mode%d/d%v/plane%d", format, field, mode, denoms, component), func(t *testing.T) {
							got, s := pWeightFixture(t, format, component >= 0, field, max(0, component), mode, denoms[0], denoms[1])
							want, _ := pWeightFixture(t, format, component >= 0, field, max(0, component), mode, denoms[0], denoms[1])
							ref := 1
							if field {
								ref = 2
							}
							s.applyPWeights(got, 0, 0, 4, 4, 8, 8, ref)
							pWeightReference(want, s, 4, 4, 8, 8, ref)
							for c := range 3 {
								if !slices.Equal(got.pic.Planes[c].Data, want.pic.Planes[c].Data) || !slices.Equal(got.pic.Planes[c].Data16, want.pic.Planes[c].Data16) {
									t.Fatalf("平面%d或未写区域不一致", c)
								}
							}
						})
					}
				}
			}
		}
	}
}

func BenchmarkPWeights_16_Auto(b *testing.B) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		for mode := range 4 {
			b.Run(fmt.Sprintf("%s/mode%d", format, mode), func(b *testing.B) {
				fs, s := pWeightFixture(b, format, false, false, 0, mode, 5, 2)
				var seed [3][]byte
				var high [3][]uint16
				for c := range 3 {
					seed[c] = slices.Clone(fs.pic.Planes[c].Data)
					high[c] = slices.Clone(fs.pic.Planes[c].Data16)
				}
				b.ReportAllocs()
				for b.Loop() {
					for c := range 3 {
						copy(fs.pic.Planes[c].Data, seed[c])
						copy(fs.pic.Planes[c].Data16, high[c])
					}
					s.applyPWeights(fs, 0, 0, 0, 0, 16, 16, 1)
				}
			})
		}
	}
}
