package syntax

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func TestHigh10SPS_ExplicitDepthAndGeometry(t *testing.T) {
	rbsp := encodeTestSPS(t, testSPS{profile: 110, chroma: 1, lumaDepth: 2, chromaDepth: 2, frameMbsOnly: true, widthMbsMinus1: 1, heightMapMinus1: 1, pocType: 2})
	s, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatal(err)
	}
	w, ew := s.DisplayWidth()
	h, eh := s.DisplayHeight()
	if s.ProfileIDC != 110 || s.BitDepthLumaMinus8 != 2 || s.BitDepthChromaMinus8 != 2 || w != 32 || h != 32 || ew != nil || eh != nil {
		t.Fatalf("High10 SPS: %+v %dx%d %v %v", s, w, h, ew, eh)
	}
	if _, err := s.MarshalRBSP(); err != nil {
		t.Fatal(err)
	}
}

func TestHigh10SliceQP_NegativeSyntaxRange(t *testing.T) {
	s := &SPS{ProfileIDC: 110, ChromaFormatIDC: 1, BitDepthLumaMinus8: 2, BitDepthChromaMinus8: 2, FrameMbsOnlyFlag: true, PicOrderCntType: 2}
	p := &PPS{PicInitQPMinus26: -38}
	if _, err := p.MarshalRBSP(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		delta   int32
		want    int
		invalid bool
	}{{0, -12, false}, {63, 51, false}, {-1, 0, true}, {64, 0, true}} {
		w := bitstream.NewWriter()
		writeUEOrFail(t, w, 0)
		writeUEOrFail(t, w, 2)
		writeUEOrFail(t, w, 0)
		writeOrFail(t, w.WriteBits(4, 0))
		writeUEOrFail(t, w, 0)
		writeOrFail(t, w.WriteBit(0))
		writeOrFail(t, w.WriteBit(0))
		writeOrFail(t, w.WriteSE(tc.delta))
		h, err := ParseSliceHeaderEx(w.Bytes(), s, map[uint32]*PPS{0: p}, true, 3)
		if tc.invalid {
			if !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("delta %d: %v", tc.delta, err)
			}
		} else if err != nil || h.SliceQP != tc.want {
			t.Fatalf("delta %d: %+v %v", tc.delta, h, err)
		}
	}
}
