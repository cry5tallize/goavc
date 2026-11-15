package syntax

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

// partitionBits 使用手工指定的码字，避免用被测语法的写入器生成期望值。
func partitionBits(t *testing.T, bits string, typ nal.UnitType, ref uint8) DataPartitionInput {
	t.Helper()
	bits = strings.ReplaceAll(bits, " ", "")
	data := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		switch b {
		case '1':
			data[i/8] |= 1 << (7 - uint(i%8))
		case '0':
		default:
			t.Fatalf("非法测试比特 %q", b)
		}
	}
	return DataPartitionInput{RBSP: data, BitLength: uint64(len(bits)), Header: nal.Header{Type: typ, RefIDC: ref}}
}

func partitionParams() (*SPS, map[uint32]*PPS) {
	sps := &SPS{
		SeqParameterSetID: 0, FrameMbsOnlyFlag: true,
		PicWidthInMbsMinus1: 3, PicHeightInMapUnitsMinus1: 1,
		PicOrderCntType: 0,
	}
	return sps, map[uint32]*PPS{0: {PicParameterSetID: 0, SeqParameterSetID: 0}}
}

func TestDataPartitionAIndependentVector(t *testing.T) {
	sps, pps := partitionParams()
	// first_mb=0, I=2, pps=0, frame_num=3, poc_lsb=10,
	// adaptive_ref_pic_marking=0, slice_qp_delta=0, slice_id=5；后面是未解析数据。
	in := partitionBits(t, "1 011 1 0011 1010 0 1 00110 101011", nal.TypeSlicePartitionA, 2)
	before := append([]byte(nil), in.RBSP...)
	a, err := ParseDataPartitionA(in, sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if a.SliceHeader.HeaderBits != 15 || a.DataBitOffset != 20 || a.Context.SliceID != 5 {
		t.Fatalf("分区A边界/ID错误: %+v / %+v", a, a.Context)
	}
	if a.SliceHeader.IDR || a.SliceHeader.FrameNum != 3 || a.SliceHeader.PicOrderCntLsb != 10 || a.SliceHeader.Type != SliceTypeI {
		t.Fatalf("分区A没有保留真实slice header: %+v", a.SliceHeader)
	}
	if a.Context.Header != in.Header || !bytes.Equal(before, in.RBSP) {
		t.Fatal("NAL头丢失或原始RBSP被改写")
	}
}

func TestDataPartitionBCConditionalVectors(t *testing.T) {
	// 同一RBSP：slice_id=5；启用色平面时colour_plane_id=2；
	// 同时启用冗余计数时redundant_pic_cnt=1。其余位始终留给数据。
	for _, typ := range []nal.UnitType{nal.TypeSlicePartitionB, nal.TypeSlicePartitionC} {
		for _, tc := range []struct {
			name      string
			plane     bool
			redundant bool
			wantPlane uint8
			wantCount uint32
			wantBits  uint64
		}{
			{"无条件字段", false, false, 0, 0, 5},
			{"仅色平面", true, false, 2, 0, 7},
			{"仅冗余计数", false, true, 0, 0, 6},
			{"两个条件字段", true, true, 2, 1, 10},
		} {
			t.Run(typ.String()+"/"+tc.name, func(t *testing.T) {
				in := partitionBits(t, "00110 10 010 111001", typ, 1)
				before := append([]byte(nil), in.RBSP...)
				ctx := DataPartitionContext{
					Header: nal.Header{Type: nal.TypeSlicePartitionA, RefIDC: 3}, SliceID: 5,
					SeparateColourPlaneFlag: tc.plane, ColourPlaneID: tc.wantPlane,
					RedundantPicCntPresentFlag: tc.redundant, RedundantPicCnt: tc.wantCount,
				}
				h, err := ParseDataPartitionBC(in, ctx)
				if err != nil {
					t.Fatal(err)
				}
				if h.Type != typ || h.SliceID != 5 || h.ColourPlaneID != tc.wantPlane ||
					h.RedundantPicCnt != tc.wantCount || h.DataBitOffset != tc.wantBits {
					t.Fatalf("条件前缀解析错误: %+v", h)
				}
				if !bytes.Equal(before, in.RBSP) {
					t.Fatal("B/C改写了原始RBSP")
				}
			})
		}
	}
}

func TestDataPartitionAContexts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bits       string
		ref        uint8
		typ        SliceType
		headerBits uint64
		id         uint32
	}{
		{"非参考P", "1 1 1 0001 0010 0 0 1 011 101", 0, SliceTypeP, 14, 2},
		{"参考B", "1 010 1 0000 0001 1 0 0 0 0 1 010 101", 1, SliceTypeB, 19, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sps, pps := partitionParams()
			h, err := ParseDataPartitionA(partitionBits(t, tc.bits, nal.TypeSlicePartitionA, tc.ref), sps, pps)
			if err != nil {
				t.Fatal(err)
			}
			if h.SliceHeader.Type != tc.typ || h.SliceHeader.HeaderBits != tc.headerBits || h.Context.SliceID != tc.id || h.DataBitOffset != tc.headerBits+3 {
				t.Fatalf("参考头解析错误: %+v / %+v", h, h.SliceHeader)
			}
		})
	}

	sps, pps := partitionParams()
	sps.SeparateColourPlaneFlag = true
	sps.ChromaFormatIDC = 3
	pps[0].RedundantPicCntPresentFlag = true
	in := partitionBits(t, "1 011 1 10 0011 1010 010 0 1 00110 111", nal.TypeSlicePartitionA, 2)
	a, err := ParseDataPartitionA(in, sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if a.SliceHeader.HeaderBits != 20 || a.DataBitOffset != 25 || !a.Context.SeparateColourPlaneFlag ||
		a.Context.ColourPlaneID != 2 || !a.Context.RedundantPicCntPresentFlag || a.Context.RedundantPicCnt != 1 {
		t.Fatalf("A未保存条件上下文: %+v", a)
	}
	// 参数集不再保活；用A的快照关联B，验证不是重复读取当前参数集。
	sps.SeparateColourPlaneFlag = false
	pps[0].RedundantPicCntPresentFlag = false
	b, err := ParseDataPartitionBC(partitionBits(t, "00110 10 010 101", nal.TypeSlicePartitionB, 1), a.Context)
	if err != nil || b == nil || b.DataBitOffset != 10 {
		t.Fatalf("A到B上下文关联失败: %+v, %v", b, err)
	}
}

func TestDataPartitionEveryBitTruncation(t *testing.T) {
	sps, pps := partitionParams()
	a := partitionBits(t, "1 011 1 0011 1010 0 1 00110", nal.TypeSlicePartitionA, 2)
	for n := uint64(0); n < 20; n++ {
		cut := a
		cut.BitLength = n
		if got, err := ParseDataPartitionA(cut, sps, pps); !errors.Is(err, h264.ErrInvalidData) || got != nil {
			t.Fatalf("A截断至%d位得到 %+v, %v", n, got, err)
		}
	}
	if _, err := ParseDataPartitionA(a, sps, pps); err != nil {
		t.Fatalf("完整A前缀被误判截断: %v", err)
	}
	for _, typ := range []nal.UnitType{nal.TypeSlicePartitionB, nal.TypeSlicePartitionC} {
		in := partitionBits(t, "00110 10 010", typ, 2)
		ctx := DataPartitionContext{
			Header: nal.Header{Type: nal.TypeSlicePartitionA, RefIDC: 2}, SliceID: 5,
			SeparateColourPlaneFlag: true, ColourPlaneID: 2,
			RedundantPicCntPresentFlag: true, RedundantPicCnt: 1,
		}
		for n := uint64(0); n < 10; n++ {
			cut := in
			cut.BitLength = n
			if got, err := ParseDataPartitionBC(cut, ctx); !errors.Is(err, h264.ErrInvalidData) || got != nil {
				t.Fatalf("%s截断至%d位得到 %+v, %v", typ, n, got, err)
			}
		}
		if _, err := ParseDataPartitionBC(in, ctx); err != nil {
			t.Fatalf("完整%s前缀被误判截断: %v", typ, err)
		}
	}
}

func TestDataPartitionInvalidHeadersAndContexts(t *testing.T) {
	sps, pps := partitionParams()
	validA := partitionBits(t, "1 011 1 0011 1010 0 1 00110", nal.TypeSlicePartitionA, 2)
	for _, header := range []nal.Header{
		{Type: nal.TypeSlice}, {Type: nal.TypeSliceIDR, RefIDC: 3},
		{Type: nal.TypeSlicePartitionB}, {Type: nal.TypeSlicePartitionC},
		{Type: nal.TypeSlicePartitionA, RefIDC: 4},
		{Type: nal.TypeSlicePartitionA, ForbiddenZeroBit: true},
	} {
		in := validA
		in.Header = header
		if got, err := ParseDataPartitionA(in, sps, pps); !errors.Is(err, h264.ErrInvalidData) || got != nil {
			t.Fatalf("A接受非法NAL头 %+v: %+v, %v", header, got, err)
		}
	}
	validBC := partitionBits(t, "00110 10 010", nal.TypeSlicePartitionB, 2)
	ctx := DataPartitionContext{
		Header: nal.Header{Type: nal.TypeSlicePartitionA, RefIDC: 2}, SliceID: 5,
		SeparateColourPlaneFlag: true, ColourPlaneID: 2,
		RedundantPicCntPresentFlag: true, RedundantPicCnt: 1,
	}
	for _, mutate := range []func(*DataPartitionInput, *DataPartitionContext){
		func(in *DataPartitionInput, _ *DataPartitionContext) { in.Header.Type = nal.TypeSlicePartitionA },
		func(in *DataPartitionInput, _ *DataPartitionContext) { in.Header.Type = nal.TypeSliceIDR },
		func(in *DataPartitionInput, _ *DataPartitionContext) { in.Header.ForbiddenZeroBit = true },
		func(in *DataPartitionInput, _ *DataPartitionContext) { in.Header.RefIDC = 4 },
		func(in *DataPartitionInput, _ *DataPartitionContext) { in.Header.RefIDC = 0 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.Header.RefIDC = 0 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.Header.RefIDC = 4 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.Header.Type = nal.TypeSlice },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.Header.ForbiddenZeroBit = true },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.SliceID = 4 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.ColourPlaneID = 1 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.ColourPlaneID = 3 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.SeparateColourPlaneFlag = false },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.RedundantPicCnt = 2 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.RedundantPicCnt = 128 },
		func(_ *DataPartitionInput, c *DataPartitionContext) { c.RedundantPicCntPresentFlag = false },
		func(in *DataPartitionInput, _ *DataPartitionContext) { in.BitLength = uint64(len(in.RBSP))*8 + 1 },
	} {
		in, badCtx := validBC, ctx
		mutate(&in, &badCtx)
		if got, err := ParseDataPartitionBC(in, badCtx); !errors.Is(err, h264.ErrInvalidData) || got != nil {
			t.Fatalf("B/C接受非法头或上下文 %+v / %+v: %+v, %v", in, badCtx, got, err)
		}
	}
	validBC.Header.RefIDC = 0
	ctx.Header.RefIDC = 0
	if _, err := ParseDataPartitionBC(validBC, ctx); err != nil {
		t.Fatalf("匹配的非参考B/C被拒绝: %v", err)
	}
}

func TestDataPartitionBCInvalidValues(t *testing.T) {
	ctx := DataPartitionContext{
		Header: nal.Header{Type: nal.TypeSlicePartitionA, RefIDC: 1}, SliceID: 5,
		SeparateColourPlaneFlag: true, ColourPlaneID: 2,
		RedundantPicCntPresentFlag: true, RedundantPicCnt: 1,
	}
	for _, bits := range []string{
		"00110 11 010",                // 色平面3非法。
		"00110 01 010",                // 色平面与A不匹配。
		"00110 10 011",                // 冗余计数2与A不匹配。
		"00110 10 00000001 0000000",   // 冗余计数127，合法范围但与A不匹配。
		"00110 10 00000001 0000001",   // 冗余计数128超界。
		"00101 10 010",                // slice_id=4，与A不匹配。
		strings.Repeat("0", 33) + "1", // ue前导零超过32位。
		strings.Repeat("0", 32) + "1" + strings.Repeat("0", 31) + "1", // ue溢出uint32。
	} {
		for _, typ := range []nal.UnitType{nal.TypeSlicePartitionB, nal.TypeSlicePartitionC} {
			if got, err := ParseDataPartitionBC(partitionBits(t, bits, typ, 1), ctx); !errors.Is(err, h264.ErrInvalidData) || got != nil {
				t.Fatalf("%s接受非法值 %s: %+v, %v", typ, bits, got, err)
			}
		}
	}
	ctx.RedundantPicCnt = 127
	if got, err := ParseDataPartitionBC(partitionBits(t, "00110 10 00000001 0000000", nal.TypeSlicePartitionC, 1), ctx); err != nil || got == nil || got.RedundantPicCnt != 127 || got.DataBitOffset != 22 {
		t.Fatalf("合法冗余计数上界失败: %+v, %v", got, err)
	}
}

func TestDataPartitionASliceIDBounds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		interlaced    bool
		mbaff         bool
		fieldBits     string
		lastIDBits    string
		invalidIDBits string
	}{
		{"逐行帧", false, false, "", "0001000", "0001001"},     // 8个宏块，7/8。
		{"隔行帧", true, false, "0", "000010000", "000010001"}, // 16个宏块，15/16。
		{"MBAFF帧", true, true, "0", "0001000", "0001001"},   // 8个宏块对，7/8。
		{"场图像", true, false, "1 0", "0001000", "0001001"},   // 8个宏块，7/8。
	} {
		t.Run(tc.name, func(t *testing.T) {
			sps, pps := partitionParams()
			sps.FrameMbsOnlyFlag = !tc.interlaced
			sps.MbAdaptiveFrameFieldFlag = tc.mbaff
			prefix := "1 011 1 0011 " + tc.fieldBits + " 1010 0 1 "
			if _, err := ParseDataPartitionA(partitionBits(t, prefix+tc.lastIDBits, nal.TypeSlicePartitionA, 1), sps, pps); err != nil {
				t.Fatalf("合法slice_id上界失败: %v", err)
			}
			if got, err := ParseDataPartitionA(partitionBits(t, prefix+tc.invalidIDBits, nal.TypeSlicePartitionA, 1), sps, pps); !errors.Is(err, h264.ErrInvalidData) || got != nil {
				t.Fatalf("slice_id越界未拒绝: %+v, %v", got, err)
			}
		})
	}
}

func TestDataPartitionAParameterErrors(t *testing.T) {
	sps, pps := partitionParams()
	in := partitionBits(t, "1 011 1 0011 1010 0 1 00110", nal.TypeSlicePartitionA, 2)
	for _, lookup := range []map[uint32]*PPS{
		{}, {0: nil}, {0: {PicParameterSetID: 0, SeqParameterSetID: 1}},
		{0: {PicParameterSetID: 1, SeqParameterSetID: 0}},
	} {
		if got, err := ParseDataPartitionA(in, sps, lookup); !errors.Is(err, h264.ErrInvalidData) || got != nil {
			t.Fatalf("A接受非法参数集关联: %+v, %v", got, err)
		}
	}
	for _, tc := range []struct {
		sps *SPS
		pps map[uint32]*PPS
	}{{nil, pps}, {sps, nil}} {
		if got, err := ParseDataPartitionA(in, tc.sps, tc.pps); err == nil || got != nil {
			t.Fatalf("缺失参数集未返回错误: %+v, %v", got, err)
		}
	}
	pps[0].EntropyCodingModeFlag = true
	if got, err := ParseDataPartitionA(in, sps, pps); !errors.Is(err, h264.ErrInvalidData) || got != nil {
		t.Fatalf("数据分区接受CABAC: %+v, %v", got, err)
	}
	pps[0].EntropyCodingModeFlag = false
	in.BitLength = uint64(len(in.RBSP))*8 + 1
	if got, err := ParseDataPartitionA(in, sps, pps); !errors.Is(err, h264.ErrInvalidData) || got != nil {
		t.Fatalf("A接受越界有效位数: %+v, %v", got, err)
	}
}

func TestDataPartitionMinimumAndPPSSelection(t *testing.T) {
	sps, _ := partitionParams()
	pps := map[uint32]*PPS{2: {PicParameterSetID: 2, SeqParameterSetID: 0}}
	// PPS ID=2，slice_id=0；没有字节对齐填充参与前缀解析。
	in := partitionBits(t, "1 011 011 0011 1010 0 1 1", nal.TypeSlicePartitionA, 3)
	a, err := ParseDataPartitionA(in, sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if a.SliceHeader.PicParameterSetID != 2 || a.SliceHeader.HeaderBits != 17 || a.DataBitOffset != 18 || a.Context.SliceID != 0 {
		t.Fatalf("非零PPS或最小slice_id错误: %+v", a)
	}
	for _, typ := range []nal.UnitType{nal.TypeSlicePartitionB, nal.TypeSlicePartitionC} {
		b, err := ParseDataPartitionBC(partitionBits(t, "1", typ, 1), a.Context)
		if err != nil || b == nil || b.SliceID != 0 || b.DataBitOffset != 1 {
			t.Fatalf("最小%s前缀错误: %+v, %v", typ, b, err)
		}
	}
}

func TestDataPartitionAConditionalTruncation(t *testing.T) {
	sps, pps := partitionParams()
	sps.SeparateColourPlaneFlag = true
	sps.ChromaFormatIDC = 3
	pps[0].RedundantPicCntPresentFlag = true
	in := partitionBits(t, "1 011 1 10 0011 1010 010 0 1 00110", nal.TypeSlicePartitionA, 2)
	for n := uint64(0); n < 25; n++ {
		cut := in
		cut.BitLength = n
		if got, err := ParseDataPartitionA(cut, sps, pps); !errors.Is(err, h264.ErrInvalidData) || got != nil {
			t.Fatalf("条件A截断至%d位得到 %+v, %v", n, got, err)
		}
	}
	if _, err := ParseDataPartitionA(in, sps, pps); err != nil {
		t.Fatalf("完整条件A前缀失败: %v", err)
	}
	for _, bits := range []string{
		"1 011 1 11 0011 1010 010 0 1 00110",              // colour_plane_id=3。
		"1 011 1 10 0011 1010 00000001 0000001 0 1 00110", // redundant_pic_cnt=128。
	} {
		if got, err := ParseDataPartitionA(partitionBits(t, bits, nal.TypeSlicePartitionA, 2), sps, pps); !errors.Is(err, h264.ErrInvalidData) || got != nil {
			t.Fatalf("A接受非法条件值: %+v, %v", got, err)
		}
	}
}

func TestDataPartitionTruncationNamesConditionalField(t *testing.T) {
	ctx := DataPartitionContext{
		Header: nal.Header{Type: nal.TypeSlicePartitionA, RefIDC: 1}, SliceID: 5,
		SeparateColourPlaneFlag: true, ColourPlaneID: 2,
		RedundantPicCntPresentFlag: true, RedundantPicCnt: 1,
	}
	in := partitionBits(t, "00110 10 010", nal.TypeSlicePartitionC, 1)
	for _, tc := range []struct {
		bits  uint64
		field string
	}{
		{4, "slice_id"}, {6, "colour_plane_id"}, {9, "redundant_pic_cnt"},
	} {
		cut := in
		cut.BitLength = tc.bits
		_, err := ParseDataPartitionBC(cut, ctx)
		if !errors.Is(err, h264.ErrInvalidData) || !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("截断未定位到%s: %v", tc.field, err)
		}
	}
}
