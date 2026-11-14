package syntax

import (
	"errors"
	"math"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
)

func TestPPSContextIDMustMatch(t *testing.T) {
	s := fmoTestSPS()
	p := &PPS{}
	data, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	s.SeqParameterSetID = 1
	if got, err := ParsePPS(data, s); !errors.Is(err, h264.ErrInvalidData) || got != nil {
		t.Fatalf("不能借用其它SPS解析FMO/scaling尺寸: %+v / %v", got, err)
	}
	w := bitstream.NewWriter()
	writeBitsOrFail(t, w, 3, 5)
	if err := p.WriteTo(w, s); !errors.Is(err, h264.ErrInvalidData) || w.BitsWritten() != 3 {
		t.Fatalf("不匹配的SPS必须在写位前拒绝: bits=%d / %v", w.BitsWritten(), err)
	}
}

func TestAdvancedSyntaxContextErrorClassification(t *testing.T) {
	in := partitionBits(t, "1 011 1 0011 0 1 1", nal.TypeSlicePartitionA, 0)
	for _, s := range []*SPS{
		{FrameMbsOnlyFlag: true, PicWidthInMbsMinus1: math.MaxUint32},
		{PicHeightInMapUnitsMinus1: 256},
	} {
		if h, err := ParseDataPartitionA(in, s, ppsMap(&PPS{})); !errors.Is(err, h264.ErrResourceLimit) || errors.Is(err, h264.ErrInvalidData) || h != nil {
			t.Fatalf("分区在分配/乘法前拒绝超限尺寸: %+v / %v", h, err)
		}
	}
	for _, mutate := range []func(*SPS){
		func(s *SPS) { s.Log2MaxFrameNumMinus4 = math.MaxUint32 },
		func(s *SPS) { s.PicOrderCntType = 3 },
		func(s *SPS) { s.PicOrderCntType = 0; s.Log2MaxPicOrderCntLsbMinus4 = math.MaxUint32 },
	} {
		s := fmoTestSPS()
		mutate(s)
		if h, err := ParseSliceHeaderEx(in.RBSP, s, ppsMap(&PPS{}), false, 0); !errors.Is(err, h264.ErrInvalidData) || h != nil {
			t.Fatalf("不能截窄溢出的SPS位宽: %+v / %v", h, err)
		}
	}
	for _, tc := range []struct {
		idr bool
		ref uint8
	}{{false, 4}, {true, 0}} {
		if h, err := ParseSliceHeaderEx(in.RBSP, fmoTestSPS(), ppsMap(&PPS{}), tc.idr, tc.ref); !errors.Is(err, h264.ErrInvalidData) || h != nil {
			t.Fatalf("非法NAL参考属性: %+v / %v", h, err)
		}
	}
	if h, err := ParseSliceHeaderEx(in.RBSP, fmoTestSPS(), map[uint32]*PPS{0: {PicParameterSetID: 1}}, false, 0); !errors.Is(err, h264.ErrInvalidData) || h != nil {
		t.Fatalf("PPS lookup必须与其键一致: %+v / %v", h, err)
	}
}

func TestSliceRedundantCountBoundaryIndependent(t *testing.T) {
	s := fmoTestSPS()
	p := &PPS{RedundantPicCntPresentFlag: true}
	for _, tc := range []struct {
		code  string
		count uint32
	}{
		{"1", 0}, {"010", 1}, {"000000010000000", 127}, {"000000010000001", 128},
	} {
		in := partitionBits(t, "1 011 1 0011 "+tc.code+" 1 101011", nal.TypeSlice, 0)
		h, err := ParseSliceHeaderEx(in.RBSP, s, ppsMap(p), false, 0)
		if tc.count > 127 {
			if !errors.Is(err, h264.ErrInvalidData) || h != nil {
				t.Fatalf("冗余计数128不是Unsupported: %+v / %v", h, err)
			}
			continue
		}
		if err != nil || h.RedundantPicCnt != tc.count || h.HeaderBits != uint64(10+len(tc.code)) {
			t.Fatalf("冗余计数及payload偏移: %+v / %v", h, err)
		}
	}
}
