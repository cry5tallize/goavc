package syntax

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func testSPSForSlice() *SPS {
	return &SPS{
		ProfileIDC:                  66,
		SeqParameterSetID:           0,
		Log2MaxFrameNumMinus4:       0,
		PicOrderCntType:             0,
		Log2MaxPicOrderCntLsbMinus4: 0,
		FrameMbsOnlyFlag:            true,
	}
}

func testPPSForSlice() *PPS {
	return &PPS{
		PicParameterSetID: 0,
		SeqParameterSetID: 0,
	}
}

func ppsMap(p *PPS) map[uint32]*PPS {
	return map[uint32]*PPS{p.PicParameterSetID: p}
}

func TestParseSliceHeader_BaselineIDR(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	w := bitstream.NewWriter()
	if err := w.WriteUE(0); err != nil { // first_mb_in_slice
		t.Fatal(err)
	}
	if err := w.WriteUE(2); err != nil { // slice_type I
		t.Fatal(err)
	}
	if err := w.WriteUE(0); err != nil { // pps id
		t.Fatal(err)
	}
	if err := w.WriteBits(4, 3); err != nil { // frame_num
		t.Fatal(err)
	}
	if err := w.WriteUE(5); err != nil { // idr_pic_id
		t.Fatal(err)
	}
	if err := w.WriteBits(4, 10); err != nil { // pic_order_cnt_lsb
		t.Fatal(err)
	}

	h, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), true)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if h.FirstMBInSlice != 0 || h.SliceTypeCode != 2 || h.Type != SliceTypeI {
		t.Fatalf("头部 = %+v", h)
	}
	if h.FrameNum != 3 || !h.IDR || h.IDRPicID != 5 {
		t.Fatalf("frame_num/idr = %d/%v/%d", h.FrameNum, h.IDR, h.IDRPicID)
	}
	if !h.HasPicOrderCntLsb || h.PicOrderCntLsb != 10 {
		t.Fatalf("POC = %+v", h)
	}
	if h.HasDeltaPicOrderCntBottom || h.HasDeltaPicOrderCnt {
		t.Fatalf("不应有 delta POC: %+v", h)
	}
}

func TestParseSliceHeader_DeltaPOCBottom(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	pps.BottomFieldPicOrderInFramePresentFlag = true
	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 0)      // slice_type P
	writeUEOrFail(t, w, 0)      // pps id
	writeBitsOrFail(t, w, 4, 1) // frame_num
	writeBitsOrFail(t, w, 4, 2) // POC lsb
	writeSEOrFail(t, w, -3)     // delta_pic_order_cnt_bottom

	h, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !h.HasDeltaPicOrderCntBottom || h.DeltaPicOrderCntBottom != -3 {
		t.Fatalf("delta bottom = %+v", h)
	}
}

func TestParseSliceHeader_POCType1(t *testing.T) {
	sps := testSPSForSlice()
	sps.PicOrderCntType = 1
	sps.DeltaPicOrderAlwaysZeroFlag = false
	pps := testPPSForSlice()
	pps.BottomFieldPicOrderInFramePresentFlag = true

	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 1) // slice_type B
	writeUEOrFail(t, w, 0)
	writeBitsOrFail(t, w, 4, 0) // frame_num
	writeSEOrFail(t, w, 7)      // delta_pic_order_cnt[0]
	writeSEOrFail(t, w, -7)     // delta_pic_order_cnt[1]

	h, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !h.HasDeltaPicOrderCnt || h.DeltaPicOrderCnt != [2]int32{7, -7} {
		t.Fatalf("delta POC = %+v", h)
	}
	if h.HasPicOrderCntLsb {
		t.Fatalf("POC type 1 不应有 lsb: %+v", h)
	}
}

func TestParseSliceHeader_FieldPicture(t *testing.T) {
	sps := testSPSForSlice()
	sps.FrameMbsOnlyFlag = false
	pps := testPPSForSlice()
	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 2)
	writeUEOrFail(t, w, 0)
	writeBitsOrFail(t, w, 4, 9) // frame_num
	writeBitOrFail(t, w, 1)     // field_pic_flag
	writeBitOrFail(t, w, 1)     // bottom_field_flag
	writeBitsOrFail(t, w, 4, 4) // POC lsb

	h, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !h.FieldPicFlag || !h.BottomFieldFlag {
		t.Fatalf("field flags = %+v", h)
	}
}

func TestParseSliceHeader_RedundantPicCnt(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	pps.RedundantPicCntPresentFlag = true
	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 2)
	writeUEOrFail(t, w, 0)
	writeBitsOrFail(t, w, 4, 0)
	writeBitsOrFail(t, w, 4, 0)
	writeUEOrFail(t, w, 7)

	h, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if h.RedundantPicCnt != 7 {
		t.Fatalf("redundant_pic_cnt = %d", h.RedundantPicCnt)
	}
}

func TestParseSliceHeader_Errors(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()

	t.Run("nil sps", func(t *testing.T) {
		if _, err := ParseSliceHeader(nil, nil, ppsMap(pps), false); err == nil {
			t.Fatal("nil sps 应返回错误")
		}
	})
	t.Run("nil pps map", func(t *testing.T) {
		if _, err := ParseSliceHeader(nil, sps, nil, false); err == nil {
			t.Fatal("nil pps map 应返回错误")
		}
	})
	t.Run("unknown pps", func(t *testing.T) {
		w := bitstream.NewWriter()
		writeUEOrFail(t, w, 0)
		writeUEOrFail(t, w, 2)
		writeUEOrFail(t, w, 3) // pps id 3
		if _, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("mismatched sps", func(t *testing.T) {
		other := testPPSForSlice()
		other.SeqParameterSetID = 1
		w := bitstream.NewWriter()
		writeUEOrFail(t, w, 0)
		writeUEOrFail(t, w, 2)
		writeUEOrFail(t, w, 0)
		if _, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(other), false); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("slice type 10", func(t *testing.T) {
		w := bitstream.NewWriter()
		writeUEOrFail(t, w, 0)
		writeUEOrFail(t, w, 10)
		if _, err := ParseSliceHeader(w.Bytes(), sps, ppsMap(pps), false); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		if _, err := ParseSliceHeader(nil, sps, ppsMap(pps), false); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
}

func TestSliceType_String(t *testing.T) {
	for code := uint32(0); code <= 9; code++ {
		typ, ok := sliceTypeFromCode(code)
		if !ok {
			t.Fatalf("code %d 应合法", code)
		}
		want := [...]string{"P", "B", "I", "SP", "SI"}[code%5]
		if typ.String() != want {
			t.Fatalf("code %d -> %s，期望 %s", code, typ, want)
		}
	}
	if _, ok := sliceTypeFromCode(10); ok {
		t.Fatal("code 10 应非法")
	}
	if SliceType(5).String() != "unknown" {
		t.Fatalf("非法 SliceType String = %q", SliceType(5).String())
	}
}

func writeUEOrFail(t *testing.T, w *bitstream.Writer, v uint32) {
	t.Helper()
	if err := w.WriteUE(v); err != nil {
		t.Fatalf("WriteUE(%d) 失败: %v", v, err)
	}
}

func writeSEOrFail(t *testing.T, w *bitstream.Writer, v int32) {
	t.Helper()
	if err := w.WriteSE(v); err != nil {
		t.Fatalf("WriteSE(%d) 失败: %v", v, err)
	}
}

func writeBitsOrFail(t *testing.T, w *bitstream.Writer, n uint, v uint64) {
	t.Helper()
	if err := w.WriteBits(n, v); err != nil {
		t.Fatalf("WriteBits(%d, %d) 失败: %v", n, v, err)
	}
}

func writeBitOrFail(t *testing.T, w *bitstream.Writer, v uint8) {
	t.Helper()
	if err := w.WriteBit(v); err != nil {
		t.Fatalf("WriteBit(%d) 失败: %v", v, err)
	}
}
