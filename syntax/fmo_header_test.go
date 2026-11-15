package syntax

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// cycle 位紧跟去块字段；尾部保留真实 payload，检查 HeaderBits 没有多消费。
func TestFMOSliceHeaderDynamicCycle(t *testing.T) {
	s := fmoTestSPS()
	s.PicWidthInMbsMinus1 = 4 // 15 个 map unit，rate=2，cycle 宽4位、最大8。
	for typ := uint32(3); typ <= 5; typ++ {
		p := &PPS{NumSliceGroupsMinus1: 1, SliceGroupMapType: typ, SliceGroupChangeRateMinus1: 1, DeblockingFilterControlPresentFlag: true}
		for _, cycle := range []uint64{0, 1, 7, 8, 9, 15} {
			w := bitstream.NewWriter()
			writeUEOrFail(t, w, 0)      // first_mb_in_slice
			writeUEOrFail(t, w, 2)      // I
			writeUEOrFail(t, w, 0)      // PPS
			writeBitsOrFail(t, w, 4, 9) // frame_num
			writeSEOrFail(t, w, 0)      // slice_qp_delta
			writeUEOrFail(t, w, 0)      // disable_deblocking_filter_idc
			writeSEOrFail(t, w, 0)
			writeSEOrFail(t, w, 0)
			writeBitsOrFail(t, w, 4, cycle)
			writeBitsOrFail(t, w, 7, 0x56) // slice data，不属于 header。
			h, err := ParseSliceHeaderEx(w.Bytes(), s, ppsMap(p), false, 0)
			if cycle > 8 {
				if !errors.Is(err, h264.ErrInvalidData) {
					t.Fatalf("type=%d cycle=%d err=%v", typ, cycle, err)
				}
				continue
			}
			if err != nil || h.SliceGroupChangeCycle != uint32(cycle) || h.HeaderBits != 17 || h.FrameNum != 9 {
				t.Fatalf("type=%d cycle=%d header=%+v err=%v", typ, cycle, h, err)
			}
			r := bitstream.NewReader(w.Bytes())
			if err := r.SkipBits(uint(h.HeaderBits)); err != nil {
				t.Fatal(err)
			}
			payload, err := r.ReadBits(7)
			if err != nil || payload != 0x56 {
				t.Fatalf("payload=%x err=%v", payload, err)
			}
			if _, err := ParseSliceHeaderEx(w.Bytes()[:2], s, ppsMap(p), false, 0); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("truncated cycle type=%d err=%v", typ, err)
			}
		}
	}
}

func TestFMOSliceHeaderStaticHasNoCycle(t *testing.T) {
	s := fmoTestSPS()
	for _, p := range []*PPS{
		{},
		{NumSliceGroupsMinus1: 1, RunLengthMinus1: []uint32{0, 1}},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 1},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 2, TopLeft: []uint32{0}, BottomRight: []uint32{1}},
		{NumSliceGroupsMinus1: 1, SliceGroupMapType: 6, PicSizeInMapUnitsMinus1: 11, SliceGroupID: make([]uint8, 12)},
	} {
		// 1|011|1|1001|1 = 10 位公共头，后面六位是 payload。
		h, err := ParseSliceHeaderEx([]byte{0xbc, 0xff}, s, ppsMap(p), false, 0)
		if err != nil || h.HeaderBits != 10 || h.SliceGroupChangeCycle != 0 || h.FrameNum != 9 {
			t.Fatalf("header=%+v err=%v", h, err)
		}
	}
}

// literal 位串固定字段顺序：1|011|1|plane(2)|1001|1。
// 若把 colour_plane_id 放在 frame_num 之后，独立 frame_num/位长断言会失败。
func TestSliceHeaderSeparateColourPlaneLiteral(t *testing.T) {
	s := fmoTestSPS()
	s.SeparateColourPlaneFlag = true
	p := &PPS{}
	for _, tc := range []struct {
		data  []byte
		plane uint8
	}{
		{[]byte{0xb9, 0x30}, 0},
		{[]byte{0xbb, 0x30}, 1},
		{[]byte{0xbd, 0x30}, 2},
	} {
		h, err := ParseSliceHeaderEx(tc.data, s, ppsMap(p), false, 0)
		if err != nil || h.ColourPlaneID != tc.plane || h.FrameNum != 9 || h.HeaderBits != 12 || h.SliceQP != 26 {
			t.Fatalf("plane=%d header=%+v err=%v", tc.plane, h, err)
		}
	}
	if _, err := ParseSliceHeaderEx([]byte{0xbf, 0x30}, s, ppsMap(p), false, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("reserved plane=%v", err)
	}
	if _, err := ParseSliceHeaderEx([]byte{0xbd}, s, ppsMap(p), false, 0); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("truncated plane header=%v", err)
	}
	// 不携带 colour plane 时不得读取这两位。
	s.SeparateColourPlaneFlag = false
	h, err := ParseSliceHeaderEx([]byte{0xbc, 0xc0}, s, ppsMap(p), false, 0)
	if err != nil || h.ColourPlaneID != 0 || h.FrameNum != 9 || h.HeaderBits != 10 {
		t.Fatalf("absent plane header=%+v err=%v", h, err)
	}
}
