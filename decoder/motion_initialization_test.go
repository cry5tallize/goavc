package decoder

import (
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/syntax"
	"testing"
)

func TestFrameState_MotionInitiallyUnavailable(t *testing.T) {
	d := mustDecoder(t, h264.DecoderConfig{})
	for _, b := range []bool{false, true} {
		fs, err := newFrameState(d, &syntax.SPS{ChromaFormatIDC: 1, PicWidthInMbsMinus1: 2, PicHeightInMapUnitsMinus1: 1, FrameMbsOnlyFlag: true}, &syntax.PPS{EntropyCodingModeFlag: true}, b)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range []*motion.Grid{fs.mv, fs.mv1} {
			if g == nil {
				continue
			}
			for y := range g.BlockH {
				for x := range g.BlockW {
					mv, _, ok := g.Get(x, y)
					if ok || mv != (motion.MV{}) || g.Ref[y*g.BlockW+x] != -1 {
						t.Fatal("新网格未保持初始不可用状态")
					}
				}
			}
		}
		fs.release()
	}
}
