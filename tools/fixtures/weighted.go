package main

import (
	"fmt"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/nal"
	"github.com/cry5tallize/goavc/syntax"
	"os"
)

// 两个不同 PCM 参考帧夹一个显式加权 Bi B 帧，编码顺序 I0/I4/B2。
// 使用不同的三个平面权重与偏移，让默认平均不能碰巧通过 Oracle。
func generateWeightedB(output string) {
	s := &syntax.SPS{ProfileIDC: 77, LevelIDC: 10, ChromaFormatIDC: 1, MaxNumRefFrames: 2, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true, VUIParametersPresentFlag: true, VUI: &syntax.VUI{BitstreamRestrictionFlag: true, MotionVectorsOverPicBoundariesFlag: true, Log2MaxMVLengthHorizontal: 15, Log2MaxMVLengthVertical: 15, MaxNumReorderFrames: 1, MaxDecFrameBuffering: 2}}
	p := &syntax.PPS{DeblockingFilterControlPresentFlag: true, WeightedBipredIDC: 1}
	srb, err := s.MarshalRBSP()
	must(err)
	prb, err := p.MarshalRBSP(s)
	must(err)
	var out []byte
	unit := func(typ nal.UnitType, ref uint8, data []byte) {
		out = append(out, 0, 0, 0, 1)
		out = append(out, nal.Header{Type: typ, RefIDC: ref}.Byte())
		out = append(out, bitstream.RBSPToEBSP(data)...)
	}
	unit(nal.TypeSPS, 3, srb)
	unit(nal.TypePPS, 3, prb)
	for frame := 0; frame < 3; frame++ {
		w := bitstream.NewWriter()
		ue := func(v uint32) { must(w.WriteUE(v)) }
		se := func(v int32) { must(w.WriteSE(v)) }
		bits := func(n uint, v uint64) { must(w.WriteBits(n, v)) }
		b := frame == 2
		ue(0)
		if b {
			ue(1)
		} else {
			ue(2)
		}
		ue(0)
		bits(4, uint64(frame))
		if frame == 0 {
			ue(0)
		}
		poc := frame * 4
		if b {
			poc = 2
		}
		bits(4, uint64(poc))
		if b {
			bits(1, 1)
			bits(1, 0)
			bits(1, 0)
			bits(1, 0)
			ue(1)
			ue(1)
			for _, v := range [][6]int32{{3, -4, 2, 5, 1, 6}, {1, 8, 2, -3, 3, -4}} {
				bits(1, 1)
				se(v[0])
				se(v[1])
				bits(1, 1)
				se(v[2])
				se(v[3])
				se(v[4])
				se(v[5])
			}
		} else if frame == 0 {
			bits(2, 0)
		} else {
			bits(1, 0)
		}
		se(0)
		ue(1)
		if b {
			ue(0)
			ue(3)
			se(0)
			se(0)
			se(0)
			se(0)
			ue(0)
		} else {
			ue(25)
			w.ByteAlign()
			for c := 0; c < 3; c++ {
				n := 256
				if c > 0 {
					n = 64
				}
				for i := 0; i < n; i++ {
					bits(8, uint64(20+frame*40+c*25+i%7))
				}
			}
		}
		must(bitstream.WriteRBSPTrailingBits(w))
		typ, ref := nal.TypeSlice, uint8(3)
		if frame == 0 {
			typ = nal.TypeSliceIDR
		}
		if b {
			ref = 0
		}
		unit(typ, ref, w.Bytes())
	}
	must(os.WriteFile(output, out, 0644))
	fmt.Printf("generated %s (%d bytes, 3 frames)\n", output, len(out))
}
