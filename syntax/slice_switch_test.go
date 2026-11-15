package syntax

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/nal"
)

// 独立码字验证 SP/SI 的条件字段顺序，末尾 payload 不能被当成头部消费。
func TestSwitchSliceIndependentVectors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bits       string
		typ        SliceType
		switchFlag bool
		qs         int
		qsDelta    int32
		headerBits uint64
	}{
		{"SP", "1 00100 1 0011 0 0 1 1 010 101011", SliceTypeSP, true, 27, 1, 18},
		{"SI", "1 00101 1 0011 1 011 101011", SliceTypeSI, false, 25, -1, 15},
		{"SP统一类型", "1 0001001 1 0011 0 0 1 0 1 101011", SliceTypeSP, false, 26, 0, 18},
		{"SI统一类型", "1 0001010 1 0011 1 1 101011", SliceTypeSI, false, 26, 0, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fmoTestSPS()
			p := &PPS{}
			in := partitionBits(t, tc.bits, nal.TypeSlice, 0)
			h, err := ParseSliceHeaderEx(in.RBSP, s, ppsMap(p), false, 0)
			if err != nil {
				t.Fatal(err)
			}
			if h.Type != tc.typ || h.SPForSwitchFlag != tc.switchFlag || h.SliceQS != tc.qs || h.SliceQSDelta != tc.qsDelta || h.SliceQP != 26 || h.HeaderBits != tc.headerBits {
				t.Fatalf("切换头部错误: %+v", h)
			}
			r := bitstream.NewReader(in.RBSP)
			if err := r.SkipBits(uint(h.HeaderBits)); err != nil {
				t.Fatal(err)
			}
			if payload, err := r.ReadBits(6); err != nil || payload != 0x2b {
				t.Fatalf("payload=%x err=%v", payload, err)
			}
		})
	}
}

// SP 的 list0/weight table 与 P 相同；SI 不能消费这些字段，也没有 CABAC 初始化。
func TestSwitchSliceReferenceWeightsAndCABAC(t *testing.T) {
	s := fmoTestSPS()
	s.ChromaFormatIDC = 1
	p := &PPS{WeightedPredFlag: true, EntropyCodingModeFlag: true, PicInitQSMinus26: -2, DeblockingFilterControlPresentFlag: true, NumSliceGroupsMinus1: 1, SliceGroupMapType: 4, SliceGroupChangeRateMinus1: 4}
	w := bitstream.NewWriter()
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 3)
	writeUEOrFail(t, w, 0)
	writeBitsOrFail(t, w, 4, 6)
	writeBitOrFail(t, w, 1) // override，两个 list0 引用。
	writeUEOrFail(t, w, 1)
	writeBitOrFail(t, w, 1) // list0 重排。
	writeUEOrFail(t, w, 0)
	writeUEOrFail(t, w, 2)
	writeUEOrFail(t, w, 3)
	writeUEOrFail(t, w, 0) // luma/chroma denom。
	writeUEOrFail(t, w, 0)
	for range 2 {
		writeBitOrFail(t, w, 0)
		writeBitOrFail(t, w, 0)
	}
	writeBitOrFail(t, w, 0) // adaptive_ref_pic_marking。
	writeUEOrFail(t, w, 2)  // cabac_init_idc。
	writeSEOrFail(t, w, 1)
	writeBitOrFail(t, w, 1) // sp_for_switch_flag。
	writeSEOrFail(t, w, -2)
	writeUEOrFail(t, w, 1)      // 关闭去块，无 offset。
	writeBitsOrFail(t, w, 2, 3) // cycle=3，图像最后一轮。
	wantBits := w.BitsWritten()
	writeBitsOrFail(t, w, 7, 0x56)
	h, err := ParseSliceHeaderEx(w.Bytes(), s, ppsMap(p), false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if h.HeaderBits != wantBits || h.Type != SliceTypeSP || h.CabacInitIDC != 2 || h.SliceQS != 22 || h.SliceQP != 27 || !h.SPForSwitchFlag || h.SliceGroupChangeCycle != 3 || h.DisableDeblockingFilterIDC != 1 {
		t.Fatalf("SP头字段错位: %+v", h)
	}
	if h.NumRefIdxL0ActiveMinus1 != 1 || len(h.RefListModifications[0]) != 1 || h.RefListModifications[0][0] != (RefListModification{IDC: 0, Value: 2}) || !h.HasPredWeightTable || len(h.PredictionWeights.Lists[0]) != 2 || h.PredictionWeights.Lists[0][1].Weight != ([3]int32{1, 1, 1}) {
		t.Fatalf("SP参考/权重丢失: %+v / %+v", h, h.PredictionWeights)
	}
	// SI：QP 后直接 QS，随后关闭去块；没有 switch flag、list0 或 cabac_init。
	p.NumSliceGroupsMinus1 = 0
	in := partitionBits(t, "1 00101 1 0011 0 1 1 010 101011", nal.TypeSlice, 1)
	h, err = ParseSliceHeaderEx(in.RBSP, s, ppsMap(p), false, 1)
	if err != nil || h.Type != SliceTypeSI || h.CabacInitIDC != 0 || h.HasPredWeightTable || h.SPForSwitchFlag || h.SliceQS != 24 || h.HeaderBits != 17 {
		t.Fatalf("SI条件字段错位: %+v / %v", h, err)
	}
}

func TestSwitchSliceQSBoundsAndTruncation(t *testing.T) {
	for _, typ := range []uint32{3, 4, 8, 9} {
		for _, qs := range []int32{-1, 0, 51, 52} {
			s := fmoTestSPS()
			s.BitDepthLumaMinus8 = 2 // QS 仍是0..51，不能沿用负QPY下限。
			p := &PPS{PicInitQSMinus26: 25}
			w := bitstream.NewWriter()
			writeUEOrFail(t, w, 0)
			writeUEOrFail(t, w, typ)
			writeUEOrFail(t, w, 0)
			writeBitsOrFail(t, w, 4, 0)
			if typ%5 == 3 {
				writeBitOrFail(t, w, 0)
				writeBitOrFail(t, w, 0)
			}
			writeSEOrFail(t, w, 0)
			if typ%5 == 3 {
				writeBitOrFail(t, w, 0)
			}
			writeSEOrFail(t, w, qs-51)
			h, err := ParseSliceHeaderEx(w.Bytes(), s, ppsMap(p), false, 0)
			if qs < 0 || qs > 51 {
				if !errors.Is(err, h264.ErrInvalidData) || h != nil {
					t.Fatalf("type=%d QS=%d: %+v / %v", typ, qs, h, err)
				}
			} else if err != nil || h.SliceQS != int(qs) {
				t.Fatalf("type=%d合法QS=%d: %+v / %v", typ, qs, h, err)
			}
		}
	}
}

func TestDataPartitionSwitchHeaderBoundaries(t *testing.T) {
	for _, bits := range []string{
		"1 00100 1 0011 0 0 1 1 010 011",
		"1 00101 1 0011 1 011 011",
	} {
		s := fmoTestSPS()
		in := partitionBits(t, bits, nal.TypeSlicePartitionA, 0)
		for n := uint64(0); n < in.BitLength; n++ {
			cut := in
			cut.BitLength = n
			if h, err := ParseDataPartitionA(cut, s, ppsMap(&PPS{})); !errors.Is(err, h264.ErrInvalidData) || h != nil {
				t.Fatalf("切换分区截断到%d位: %+v / %v", n, h, err)
			}
		}
		a, err := ParseDataPartitionA(in, s, ppsMap(&PPS{}))
		if err != nil || a.Context.SliceID != 2 || a.DataBitOffset != in.BitLength || a.SliceHeader.HeaderBits+3 != a.DataBitOffset {
			t.Fatalf("切换分区边界错误: %+v / %v", a, err)
		}
	}
}

func TestSwitchSliceIDRConditions(t *testing.T) {
	s := fmoTestSPS()
	p := &PPS{}
	// IDR允许SI，两个参考标记位在QP/QS之前，不能走SP的list0路径。
	in := partitionBits(t, "1 00101 1 0000 1 00 1 1 101011", nal.TypeSliceIDR, 3)
	h, err := ParseSliceHeaderEx(in.RBSP, s, ppsMap(p), true, 3)
	if err != nil || !h.IDR || h.Type != SliceTypeSI || h.SliceQS != 26 || h.HeaderBits != 16 {
		t.Fatalf("IDR SI字段错位: %+v / %v", h, err)
	}
	for _, code := range []uint32{3, 8} {
		w := bitstream.NewWriter()
		writeUEOrFail(t, w, 0)
		writeUEOrFail(t, w, code)
		if h, err := ParseSliceHeaderEx(w.Bytes(), s, ppsMap(p), true, 3); !errors.Is(err, h264.ErrInvalidData) || h != nil {
			t.Fatalf("SP不能用IDR NAL: %+v / %v", h, err)
		}
	}
}
