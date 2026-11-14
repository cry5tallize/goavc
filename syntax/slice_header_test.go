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

// writeDecRefPicMarking 写入 nal_ref_idc != 0 时的参考帧标记。
// 旧测试入口默认 nal_ref_idc=3，因此非 IDR 也要写 adaptive flag。
func writeDecRefPicMarking(t *testing.T, w *bitstream.Writer, idr bool) {
	t.Helper()
	if idr {
		writeBitOrFail(t, w, 0) // no_output_of_prior_pics_flag
		writeBitOrFail(t, w, 0) // long_term_reference_flag
		return
	}
	writeBitOrFail(t, w, 0) // adaptive_ref_pic_marking_mode_flag
}

// writeSliceHeaderTail 写入 P/B 切片的参考列表相关位与 slice_qp_delta。
func writeSliceHeaderTail(t *testing.T, w *bitstream.Writer, typ SliceType) {
	t.Helper()
	if typ == SliceTypeB {
		writeBitOrFail(t, w, 0) // direct_spatial_mv_pred_flag
	}
	if typ == SliceTypeP || typ == SliceTypeB {
		writeBitOrFail(t, w, 0) // num_ref_idx_active_override_flag
		writeBitOrFail(t, w, 0) // ref_pic_list_reordering_flag_l0
		if typ == SliceTypeB {
			writeBitOrFail(t, w, 0) // ref_pic_list_reordering_flag_l1
		}
	}
	writeSEOrFail(t, w, 0) // slice_qp_delta
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
	writeDecRefPicMarking(t, w, true)
	writeSliceHeaderTail(t, w, SliceTypeI)

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
	writeDecRefPicMarking(t, w, false)
	writeSliceHeaderTail(t, w, SliceTypeP)

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
	sps.ProfileIDC = 77 // B切片属于Main，不再用非法Baseline上下文。
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
	writeDecRefPicMarking(t, w, false)
	writeSliceHeaderTail(t, w, SliceTypeB)

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
	sps.ProfileIDC = 77
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
	writeDecRefPicMarking(t, w, false)
	writeSliceHeaderTail(t, w, SliceTypeI)

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
	writeDecRefPicMarking(t, w, false)
	writeSliceHeaderTail(t, w, SliceTypeI)

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

func TestParseSliceHeader_BaselineDecodingFields(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	pps.PicInitQPMinus26 = -3
	pps.DeblockingFilterControlPresentFlag = true

	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)      // first_mb
	writeUEOrFail(t, w, 0)      // slice_type P
	writeUEOrFail(t, w, 0)      // pps id
	writeBitsOrFail(t, w, 4, 2) // frame_num
	writeBitsOrFail(t, w, 4, 1) // poc_lsb
	writeBitOrFail(t, w, 1)     // num_ref_idx_active_override_flag
	writeUEOrFail(t, w, 1)      // num_ref_idx_l0_active_minus1
	writeBitOrFail(t, w, 0)     // ref_pic_list_reordering_flag_l0
	writeBitOrFail(t, w, 0)     // adaptive_ref_pic_marking_mode_flag
	writeSEOrFail(t, w, -2)     // slice_qp_delta
	writeUEOrFail(t, w, 2)      // disable_deblocking_filter_idc
	writeSEOrFail(t, w, -3)     // slice_alpha_c0_offset_div2
	writeSEOrFail(t, w, 2)      // slice_beta_offset_div2

	h, err := ParseSliceHeaderEx(w.Bytes(), sps, ppsMap(pps), false, 1)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !h.NumRefIdxActiveOverrideFlag || h.NumRefIdxL0ActiveMinus1 != 1 {
		t.Fatalf("ref idx = %+v", h)
	}
	if h.SliceQPDelta != -2 || h.SliceQP != 21 {
		t.Fatalf("QP = %d/%d, want -2/21", h.SliceQPDelta, h.SliceQP)
	}
	if h.DisableDeblockingFilterIDC != 2 || h.SliceAlphaC0OffsetDiv2 != -3 || h.SliceBetaOffsetDiv2 != 2 {
		t.Fatalf("deblock = %+v", h)
	}
	if h.HeaderBits != w.BitsWritten() {
		t.Fatalf("HeaderBits = %d, want %d", h.HeaderBits, w.BitsWritten())
	}
}

func TestParseSliceHeader_RefListReorderingConsumed(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 0) // P
	writeUEOrFail(t, w, 0)
	writeBitsOrFail(t, w, 4, 0)
	writeBitsOrFail(t, w, 4, 0)
	writeBitOrFail(t, w, 0) // override
	writeBitOrFail(t, w, 1) // reorder flag
	writeUEOrFail(t, w, 0)  // idc 0
	writeUEOrFail(t, w, 0)  // idc_value
	writeUEOrFail(t, w, 3)  // end
	writeBitOrFail(t, w, 0) // adaptive marking
	writeSEOrFail(t, w, 5)  // slice_qp_delta

	h, err := ParseSliceHeaderEx(w.Bytes(), sps, ppsMap(pps), false, 1)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !h.RefPicListReorderingL0 {
		t.Fatal("RefPicListReorderingL0 = false, want true")
	}
	if h.SliceQPDelta != 5 {
		t.Fatalf("slice_qp_delta = %d, want 5 (命令序列未正确消费)", h.SliceQPDelta)
	}
}

func TestParseSliceHeader_MMCOConsumed(t *testing.T) {
	sps := testSPSForSlice()
	pps := testPPSForSlice()
	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 0) // P
	writeUEOrFail(t, w, 0)
	writeBitsOrFail(t, w, 4, 0)
	writeBitsOrFail(t, w, 4, 0)
	writeBitOrFail(t, w, 0) // override
	writeBitOrFail(t, w, 0) // reorder
	writeBitOrFail(t, w, 1) // adaptive marking
	writeUEOrFail(t, w, 1)  // MMCO 1
	writeUEOrFail(t, w, 2)  // difference_of_pic_nums_minus1
	writeUEOrFail(t, w, 0)  // end
	writeSEOrFail(t, w, 3)  // slice_qp_delta

	h, err := ParseSliceHeaderEx(w.Bytes(), sps, ppsMap(pps), false, 1)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !h.AdaptiveRefPicMarking || h.MMCOCount != 1 {
		t.Fatalf("MMCO = %+v", h)
	}
	if len(h.MemoryManagement) != 1 || h.MemoryManagement[0].Operation != 1 || h.MemoryManagement[0].DifferenceOfPicNumsMinus1 != 2 {
		t.Fatal("MMCO operands were discarded")
	}
	if h.SliceQPDelta != 3 {
		t.Fatalf("slice_qp_delta = %d, want 3", h.SliceQPDelta)
	}
}
