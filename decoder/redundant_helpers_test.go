package decoder

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

type redundantSliceVector struct {
	count         uint32
	first, length int
	bias          int
	frame, poc    uint64
	pps           uint32
	predict       bool
}

func redundantSample(c, mb, x, y, bias int) byte {
	return byte(19 + (mb*23+c*47+x*5+y*9+bias)%211)
}

func redundantParameters(t *testing.T) []nal.Unit {
	t.Helper()
	s := &syntax.SPS{ProfileIDC: 88, LevelIDC: 20, ChromaFormatIDC: 1, MaxNumRefFrames: 1, PicWidthInMbsMinus1: 3, PicHeightInMapUnitsMinus1: 3, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true,
		VUIParametersPresentFlag: true, VUI: &syntax.VUI{BitstreamRestrictionFlag: true, MotionVectorsOverPicBoundariesFlag: true, Log2MaxMVLengthHorizontal: 15, Log2MaxMVLengthVertical: 15, MaxDecFrameBuffering: 1}}
	p := &syntax.PPS{RedundantPicCntPresentFlag: true, DeblockingFilterControlPresentFlag: true}
	srb, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	return []nal.Unit{fmoUnit(nal.TypeSPS, srb), fmoUnit(nal.TypePPS, prb)}
}

func redundantSlice(v redundantSliceVector, idr bool) nal.Unit {
	w := bitstream.NewWriter()
	w.WriteUE(uint32(v.first))
	if v.predict {
		w.WriteUE(0)
	} else {
		w.WriteUE(2)
	}
	w.WriteUE(v.pps)
	w.WriteBits(4, v.frame)
	if idr {
		w.WriteUE(0)
	}
	w.WriteBits(4, v.poc)
	w.WriteUE(v.count)
	if v.predict {
		w.WriteBit(0)
		w.WriteBit(0)
	}
	if idr {
		w.WriteBits(2, 0)
	} else {
		w.WriteBit(0)
	}
	w.WriteSE(0)
	w.WriteUE(1)
	if v.predict {
		// 独立 slice 的首宏块 MVP=0；后续整行继承左邻居。
		for i := range v.length {
			w.WriteUE(0)
			w.WriteUE(0)
			dx := int32(0)
			if i == 0 {
				dx = 8
			}
			w.WriteSE(dx)
			w.WriteSE(0)
			w.WriteUE(0)
		}
	} else {
		for mb := v.first; mb < v.first+v.length; mb++ {
			w.WriteUE(25)
			w.ByteAlign()
			for c := range 3 {
				n := 16
				if c != 0 {
					n = 8
				}
				for y := range n {
					for x := range n {
						w.WriteBits(8, uint64(redundantSample(c, mb, x, y, v.bias)))
					}
				}
			}
		}
	}
	bitstream.WriteRBSPTrailingBits(w)
	kind := nal.TypeSlice
	if idr {
		kind = nal.TypeSliceIDR
	}
	return fmoUnit(kind, w.Bytes())
}

func redundantInit(t *testing.T, d *Decoder) {
	t.Helper()
	units := redundantParameters(t)
	units = append(units, redundantSlice(redundantSliceVector{length: 16}, true))
	if err := d.SendPacket(h264.Packet{Data: annex(units...)}); err != nil {
		t.Fatal(err)
	}
	redundantAssert(t, d, 0, false)
}

func redundantAssert(t *testing.T, d *Decoder, bias int, predict bool) {
	t.Helper()
	f, err := d.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	if f.Width != 64 || f.Height != 64 || f.Format != h264.YUV420P {
		t.Fatalf("unexpected redundant frame: %dx%d %v", f.Width, f.Height, f.Format)
	}
	for c := range 3 {
		n := 16
		if c != 0 {
			n = 8
		}
		for y := range n * 4 {
			for x := range n * 4 {
				sx := x
				if predict {
					sx += 2
					if c != 0 {
						sx--
					}
					if sx >= n*4 {
						sx = n*4 - 1
					}
				}
				want := redundantSample(c, y/n*4+sx/n, sx%n, y%n, bias)
				if got := f.Planes[c].Data[y*f.Planes[c].Stride+x]; got != want {
					t.Fatalf("plane %d (%d,%d): got %d want %d", c, x, y, got, want)
				}
			}
		}
	}
}

func redundantNoFrame(t *testing.T, d *Decoder) {
	t.Helper()
	f, err := d.ReceiveFrame()
	if f != nil {
		f.Release()
	}
	if !errors.Is(err, h264.ErrAgain) {
		t.Fatalf("unexpected extra picture: %v", err)
	}
}
