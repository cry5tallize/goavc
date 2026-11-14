package syntax

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func TestSPS_FieldAndMBAFFFixedSyntax(t *testing.T) {
	// Spec: 7.3.2.1.1，Main/Level30，宽3 MB，高2 map unit；右裁2像素、下裁4像素。
	for _, tc := range []struct {
		data  []byte
		mbaff bool
	}{
		{[]byte{0x4d, 0x00, 0x1e, 0xf6, 0x68, 0xea, 0x90}, false},
		{[]byte{0x4d, 0x00, 0x1e, 0xf6, 0x69, 0xea, 0x90}, true},
	} {
		s, err := ParseSPS(tc.data)
		if err != nil {
			t.Fatal(err)
		}
		w, ew := s.DisplayWidth()
		h, eh := s.DisplayHeight()
		if s.FrameMbsOnlyFlag || s.MbAdaptiveFrameFieldFlag != tc.mbaff || s.PicHeightInMapUnits() != 2 || s.FrameHeightInMbs() != 4 || w != 46 || h != 60 || ew != nil || eh != nil {
			t.Fatalf("wrong field geometry: %+v %dx%d %v %v", s, w, h, ew, eh)
		}
		got, err := s.MarshalRBSP()
		if err != nil || !bytes.Equal(got, tc.data) {
			t.Fatalf("field SPS written %x, want %x: %v", got, tc.data, err)
		}
		for _, mutate := range []func(*SPS){
			func(s *SPS) { s.FrameMbsOnlyFlag = true; s.MbAdaptiveFrameFieldFlag = true },
			func(s *SPS) { s.Direct8x8InferenceFlag = false },
			func(s *SPS) { s.ProfileIDC = 66 },
		} {
			invalid := *s
			mutate(&invalid)
			writer := bitstream.NewWriter()
			writer.WriteBits(3, 5)
			if err := invalid.WriteTo(writer); !errors.Is(err, h264.ErrInvalidData) || writer.BitsWritten() != 3 {
				t.Fatalf("invalid field flags partially written: bits%d err%v", writer.BitsWritten(), err)
			}
		}
	}
}

func TestSliceHeader_FieldParityAndFrameDeltaFixedSyntax(t *testing.T) {
	s := &SPS{ProfileIDC: 77, ChromaFormatIDC: 1, PicWidthInMbsMinus1: 2, PicHeightInMapUnitsMinus1: 1, Direct8x8InferenceFlag: true}
	p := &PPS{BottomFieldPicOrderInFramePresentFlag: true, DeblockingFilterControlPresentFlag: true}
	for _, tc := range []struct {
		data          []byte
		field, bottom bool
		bits          uint64
	}{
		{[]byte{0xbc, 0xd6, 0x0a, 0xab, 0x40}, true, false, 26},
		{[]byte{0xbc, 0xf6, 0x0a, 0xab, 0x40}, true, true, 26},
		{[]byte{0xbc, 0xac, 0x82, 0xaa, 0xd0}, false, false, 28},
	} {
		for _, mbaff := range []bool{false, true} {
			s.MbAdaptiveFrameFieldFlag = mbaff
			h, err := ParseSliceHeaderEx(tc.data, s, map[uint32]*PPS{0: p}, true, 3)
			if err != nil {
				t.Fatal(err)
			}
			if h.FieldPicFlag != tc.field || h.BottomFieldFlag != tc.bottom || h.FrameNum != 9 || h.PicOrderCntLsb != 6 || h.SliceQP != 24 || h.HeaderBits != tc.bits || h.HasDeltaPicOrderCntBottom == tc.field || !tc.field && h.DeltaPicOrderCntBottom != 1 {
				t.Fatalf("wrong field header: %+v", h)
			}
			r := bitstream.NewReader(tc.data)
			r.SkipBits(uint(h.HeaderBits))
			payload, err := r.ReadBits(8)
			if err != nil || payload != 0xad {
				t.Fatalf("field header consumed payload: %x %v", payload, err)
			}
		}
	}
}
