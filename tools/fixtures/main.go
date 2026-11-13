// fixtures 生成专门覆盖参考列表重排、长期参考与 MMCO 1..6 的规范小码流。
// 不实现通用编码器，也不调用外部编解码库；输出随后用 FFmpeg 独立验证。
package main

import (
	"flag"
	"fmt"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
	"os"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	output := flag.String("output", "testdata/main/main_16x16_yuv420p_cavlc_mmco_006.264", "输出 Annex-B 路径")
	pocType := flag.Uint("poc-type", 0, "POC 类型 0/1/2")
	flag.Parse()
	if *pocType > 2 {
		panic("poc-type must be 0/1/2")
	}
	s := &syntax.SPS{ProfileIDC: 77, LevelIDC: 10, ChromaFormatIDC: 1, MaxNumRefFrames: 3, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	s.PicOrderCntType = uint32(*pocType)
	if *pocType == 1 {
		s.NumRefFramesInPicOrderCntCycle = 1
		s.OffsetForRefFrame = []int32{2}
		s.OffsetForNonRefPic = -1
	}
	p := &syntax.PPS{DeblockingFilterControlPresentFlag: true}
	srb, err := s.MarshalRBSP()
	must(err)
	prb, err := p.MarshalRBSP(s)
	must(err)
	var out []byte
	appendUnit := func(typ nal.UnitType, rbsp []byte) {
		out = append(out, 0, 0, 0, 1)
		out = append(out, nal.Header{Type: typ, RefIDC: 3}.Byte())
		out = append(out, bitstream.RBSPToEBSP(rbsp)...)
	}
	appendUnit(nal.TypeSPS, srb)
	appendUnit(nal.TypePPS, prb)
	frames := []struct {
		fn, poc       uint32
		pcm           bool
		active, index uint32
		reorder       []syntax.RefListModification
		ops           []syntax.MMCO
	}{
		{fn: 0, poc: 0, pcm: true},
		{fn: 1, poc: 2, pcm: true, ops: []syntax.MMCO{{Operation: 4, MaxLongTermFrameIdxPlus1: 2}, {Operation: 3, DifferenceOfPicNumsMinus1: 0, LongTermFrameIdx: 1}}},
		{fn: 2, poc: 4, active: 2, reorder: []syntax.RefListModification{{IDC: 2, Value: 1}}, ops: []syntax.MMCO{{Operation: 1, DifferenceOfPicNumsMinus1: 0}, {Operation: 6, LongTermFrameIdx: 0}}},
		{fn: 3, poc: 6, pcm: true, ops: []syntax.MMCO{{Operation: 2, LongTermPicNum: 1}, {Operation: 4, MaxLongTermFrameIdxPlus1: 1}}},
		{fn: 4, poc: 8, active: 2, index: 1, ops: []syntax.MMCO{{Operation: 5}}},
		{fn: 1, poc: 2, active: 1},
	}
	for number, f := range frames {
		w := bitstream.NewWriter()
		ue := func(v uint32) { must(w.WriteUE(v)) }
		se := func(v int32) { must(w.WriteSE(v)) }
		bits := func(n uint, v uint64) { must(w.WriteBits(n, v)) }
		ue(0)
		if f.pcm {
			ue(2)
		} else {
			ue(0)
		}
		ue(0)
		bits(4, uint64(f.fn))
		if number == 0 {
			ue(0)
		}
		if *pocType == 0 {
			bits(4, uint64(f.poc))
		} else if *pocType == 1 {
			se(int32(f.poc) - int32(f.fn*2))
		}
		if !f.pcm {
			bits(1, 1)
			ue(f.active - 1)
			bits(1, uint64(boolInt(len(f.reorder) > 0)))
			if len(f.reorder) > 0 {
				for _, r := range f.reorder {
					ue(r.IDC)
					ue(r.Value)
				}
				ue(3)
			}
		}
		if number == 0 {
			bits(2, 0)
		} else {
			bits(1, uint64(boolInt(len(f.ops) > 0)))
			if len(f.ops) > 0 {
				for _, m := range f.ops {
					ue(m.Operation)
					if m.Operation == 1 || m.Operation == 3 {
						ue(m.DifferenceOfPicNumsMinus1)
					}
					switch m.Operation {
					case 2:
						ue(m.LongTermPicNum)
					case 3, 6:
						ue(m.LongTermFrameIdx)
					case 4:
						ue(m.MaxLongTermFrameIdxPlus1)
					}
				}
				ue(0)
			}
		}
		se(0)
		ue(1)
		if f.pcm {
			ue(25)
			w.ByteAlign()
			for c := 0; c < 3; c++ {
				size := 256
				if c > 0 {
					size = 64
				}
				for i := 0; i < size; i++ {
					bits(8, uint64(20+number*15+c*25+i%7))
				}
			}
		} else {
			ue(0)
			ue(0)
			if f.active == 2 {
				bits(1, uint64(1-f.index))
			}
			se(0)
			se(0)
			ue(0)
		}
		must(bitstream.WriteRBSPTrailingBits(w))
		typ := nal.TypeSlice
		if number == 0 {
			typ = nal.TypeSliceIDR
		}
		appendUnit(typ, w.Bytes())
	}
	must(os.WriteFile(*output, out, 0644))
	fmt.Printf("generated %s (%d bytes, %d frames)\n", *output, len(out), len(frames))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
