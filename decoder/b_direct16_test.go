package decoder

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/syntax"
)

func TestBDirect16_GroupedParity(t *testing.T) {
	for _, spatial := range []bool{false, true} {
		for _, infer := range []bool{false, true} {
			for _, varying := range []bool{false, true} {
				for _, missing := range []bool{false, true} {
					t.Run(fmt.Sprintf("spatial=%t/infer=%t/vary=%t/missing=%t", spatial, infer, varying, missing), func(t *testing.T) {
						sps := &syntax.SPS{ProfileIDC: 100, LevelIDC: 10, ChromaFormatIDC: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: infer}
						pps := &syntax.PPS{}
						s := &sliceEntry{sps: sps, pps: pps, hdr: &syntax.SliceHeader{Type: syntax.SliceTypeB, DirectSpatialMVPredFlag: spatial}}
						for l := range 2 {
							p, err := picture.New(1, 1, h264.YUV420P)
							if err != nil {
								t.Fatal(err)
							}
							defer p.Release()
							for c := range p.Planes {
								pl := &p.Planes[c]
								for y := range pl.Height {
									for x := range pl.Width {
										pl.Row(y)[x] = byte(x*13 + y*9 + c*19 + l*71)
									}
								}
								pl.FillBorder()
							}
							r := &dpb.Reference{Identity: &dpb.Identity{ID: uint64(l + 1), POC: int32(l * 4)}, Picture: p}
							s.refs[l] = []*dpb.Reference{r}
						}
						r := s.refs[1][0]
						r.Motion[0] = motion.NewGrid(1, 1)
						r.BlockReferences[0] = make([]*dpb.Identity, 16)
						for i := range 16 {
							x, y := i%4, i/4
							mv := motion.MV{X: 4, Y: -4}
							if varying {
								mv = motion.MV{X: int32(x * 4), Y: int32(-y * 4)}
							}
							r.Motion[0].SetRect(x, y, 1, 1, mv, 0)
							r.BlockReferences[0][i] = s.refs[0][0].Identity
						}
						if missing {
							s.refs[0] = nil
						}
						var states [2]*frameState
						var errs [2]error
						var qps [2]int
						for i, decode := range []func(*frameState, int, int, int, int, *sliceEntry, *int, bool) error{decodeBMacroblock, decodeBGrouped} {
							d := mustDecoder(t, h264.DecoderConfig{})
							fs, err := newFrameState(d, sps, pps, true)
							if err != nil {
								t.Fatal(err)
							}
							defer fs.release()
							fs.poc = 2
							states[i] = fs
							s.data = bitstream.NewReader([]byte{0xff})
							qps[i] = 26
							errs[i] = decode(fs, 0, 0, 0, 0, s, &qps[i], true)
							if s.data.BitsRead() != 0 {
								t.Fatal("Skip 消费了残差或运动语法")
							}
						}
						a, b := states[0], states[1]
						if fmt.Sprint(errs[0]) != fmt.Sprint(errs[1]) || (errs[0] != nil) != missing || qps[0] != qps[1] ||
							!reflect.DeepEqual(a.info, b.info) || !reflect.DeepEqual(a.mv, b.mv) || !reflect.DeepEqual(a.mv1, b.mv1) ||
							!slices.Equal(a.directBlocks, b.directBlocks) || !slices.Equal(a.mvd, b.mvd) || !slices.Equal(a.mvd1, b.mvd1) ||
							!slices.Equal(a.referenceIDs[0], b.referenceIDs[0]) || !slices.Equal(a.referenceIDs[1], b.referenceIDs[1]) {
							t.Fatalf("通用和快路状态不一致: %v", errs)
						}
						for c := range a.pic.Planes {
							if !slices.Equal(a.pic.Planes[c].Data, b.pic.Planes[c].Data) {
								t.Fatalf("平面 %d 不一致", c)
							}
						}
					})
				}
			}
		}
	}
}
