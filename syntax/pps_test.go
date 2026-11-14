package syntax

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func parsePPSHex(t *testing.T, sps *SPS, hexString string) (*PPS, []byte) {
	t.Helper()
	nal := decodeHex(t, hexString)
	rbsp, err := bitstream.EBSPToRBSP(nal[1:])
	if err != nil {
		t.Fatalf("去除防竞争字节失败: %v", err)
	}
	pps, err := ParsePPS(rbsp, sps)
	if err != nil {
		t.Fatalf("解析 PPS 失败: %v", err)
	}
	return pps, rbsp
}

// TestPPS_RealCorpusRoundTrip 验证真实 x264 PPS 的逐字节往返。
func TestPPS_RealCorpusRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		spsHex string
		ppsHex string
	}{
		{
			name:   "baseline",
			spsHex: "6742c00ad9047b011000000300100000030140f1226480",
			ppsHex: "68cb83cb20",
		},
		{
			name:   "main",
			spsHex: "674d400aeca23d8088000003000800000300a078912cb0",
			ppsHex: "68ebe3cb20",
		},
		{
			name:   "high",
			spsHex: "6764000aacd9447b011000000300100000030140f1225960",
			ppsHex: "68ebe3cb22c0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sps := parseSPSHex(t, tc.spsHex)
			pps, original := parsePPSHex(t, sps, tc.ppsHex)
			got, err := pps.MarshalRBSP(sps)
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("往返字节不一致:\n got %x\nwant %x", got, original)
			}
		})
	}
}

// TestPPS_RoundTrip_NoOptionalBlock 验证省略扩展块时的默认值。
func TestPPS_RoundTrip_NoOptionalBlock(t *testing.T) {
	sps := &SPS{
		ProfileIDC:                66,
		FrameMbsOnlyFlag:          true,
		PicWidthInMbsMinus1:       3,
		PicHeightInMapUnitsMinus1: 2,
	}
	pps := &PPS{
		PicParameterSetID:                  1,
		SeqParameterSetID:                  0,
		NumRefIdxL0DefaultActiveMinus1:     0,
		NumRefIdxL1DefaultActiveMinus1:     0,
		PicInitQPMinus26:                   0,
		PicInitQSMinus26:                   0,
		ChromaQPIndexOffset:                -2,
		SecondChromaQPIndexOffset:          -2,
		DeblockingFilterControlPresentFlag: true,
	}
	rbsp, err := pps.MarshalRBSP(sps)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	parsed, err := ParsePPS(rbsp, sps)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if parsed.HasOptionalBlock {
		t.Fatal("不应检测到可选扩展块")
	}
	if parsed.SecondChromaQPIndexOffset != parsed.ChromaQPIndexOffset {
		t.Fatalf("second chroma offset = %d，期望 %d",
			parsed.SecondChromaQPIndexOffset, parsed.ChromaQPIndexOffset)
	}
	if !reflect.DeepEqual(parsed, pps) {
		t.Fatalf("结构不一致:\n got %+v\nwant %+v", parsed, pps)
	}
}

// TestPPS_RoundTrip_OptionalBlock 验证 transform_8x8 与 scaling list 扩展。
func TestPPS_RoundTrip_OptionalBlock(t *testing.T) {
	sps := &SPS{
		ProfileIDC:                100,
		ChromaFormatIDC:           1,
		FrameMbsOnlyFlag:          true,
		PicWidthInMbsMinus1:       3,
		PicHeightInMapUnitsMinus1: 2,
	}
	pps := &PPS{
		PicParameterSetID:                  0,
		SeqParameterSetID:                  0,
		NumRefIdxL0DefaultActiveMinus1:     1,
		NumRefIdxL1DefaultActiveMinus1:     1,
		WeightedPredFlag:                   true,
		WeightedBipredIDC:                  2,
		PicInitQPMinus26:                   -3,
		PicInitQSMinus26:                   1,
		ChromaQPIndexOffset:                2,
		DeblockingFilterControlPresentFlag: true,
		ConstrainedIntraPredFlag:           true,
		RedundantPicCntPresentFlag:         true,
		HasOptionalBlock:                   true,
		Transform8x8ModeFlag:               true,
		PicScalingMatrixPresentFlag:        true,
		SecondChromaQPIndexOffset:          -1,
	}
	pps.PicScalingListPresentFlag[0] = true
	pps.PicScalingListPresentFlag[6] = true
	pps.PicScalingList[0] = &ScalingList{Size: 4, Deltas: []int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}}
	pps.PicScalingList[6] = &ScalingList{Size: 8, Deltas: []int32{-8}}

	rbsp, err := pps.MarshalRBSP(sps)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	parsed, err := ParsePPS(rbsp, sps)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !parsed.HasOptionalBlock || !parsed.Transform8x8ModeFlag || !parsed.PicScalingMatrixPresentFlag {
		t.Fatalf("扩展标志丢失: %+v", parsed)
	}
	for i := 0; i < 8; i++ {
		if parsed.PicScalingListPresentFlag[i] != pps.PicScalingListPresentFlag[i] {
			t.Fatalf("scaling list flag[%d] 不一致", i)
		}
	}
	for i := 0; i < 8; i++ {
		got, want := parsed.PicScalingList[i], pps.PicScalingList[i]
		if (got == nil) != (want == nil) {
			t.Fatalf("scaling list %d 存在性不一致", i)
		}
		if got != nil && (got.Size != want.Size || !reflect.DeepEqual(got.Deltas, want.Deltas)) {
			t.Fatalf("scaling list %d 不一致", i)
		}
	}

	again, err := parsed.MarshalRBSP(sps)
	if err != nil {
		t.Fatalf("二次序列化失败: %v", err)
	}
	if !bytes.Equal(again, rbsp) {
		t.Fatalf("二次往返字节不一致")
	}
}

// TestParsePPS_FMOTruncated 验证缺少 FMO 语法仍属于截断，而非 Unsupported。
func TestParsePPS_FMOTruncated(t *testing.T) {
	w := bitstream.NewWriter()
	if err := w.WriteUE(0); err != nil { // pps id
		t.Fatal(err)
	}
	if err := w.WriteUE(0); err != nil { // sps id
		t.Fatal(err)
	}
	if err := w.WriteBit(0); err != nil { // entropy
		t.Fatal(err)
	}
	if err := w.WriteBit(0); err != nil { // bottom field
		t.Fatal(err)
	}
	if err := w.WriteUE(1); err != nil { // num_slice_groups_minus1
		t.Fatal(err)
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		t.Fatal(err)
	}
	sps := &SPS{ProfileIDC: 66, FrameMbsOnlyFlag: true}
	if _, err := ParsePPS(w.Bytes(), sps); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
	}
}

// TestPPS_Errors 验证解析与写出前的边界检查。
func TestPPS_Errors(t *testing.T) {
	sps := &SPS{ProfileIDC: 66, FrameMbsOnlyFlag: true}
	t.Run("nil sps parse", func(t *testing.T) {
		if _, err := ParsePPS(nil, nil); err == nil {
			t.Fatal("nil sps 应返回错误")
		}
	})
	t.Run("nil sps write", func(t *testing.T) {
		if _, err := (&PPS{}).MarshalRBSP(nil); err == nil {
			t.Fatal("nil sps 应返回错误")
		}
	})
	t.Run("nil pps", func(t *testing.T) {
		var p *PPS
		if _, err := p.MarshalRBSP(sps); err == nil {
			t.Fatal("nil pps 应返回错误")
		}
	})
	t.Run("bad ids", func(t *testing.T) {
		for name, mutate := range map[string]func(*PPS){
			"pps id": func(p *PPS) { p.PicParameterSetID = 256 },
			"sps id": func(p *PPS) { p.SeqParameterSetID = 32 },
		} {
			p := &PPS{}
			mutate(p)
			if _, err := p.MarshalRBSP(sps); err == nil {
				t.Fatalf("%s 应返回错误", name)
			}
		}
	})
	t.Run("bad weighted bipred", func(t *testing.T) {
		if _, err := (&PPS{WeightedBipredIDC: 3}).MarshalRBSP(sps); err == nil {
			t.Fatal("weighted_bipred_idc=3 应返回错误")
		}
	})
	t.Run("bad qp", func(t *testing.T) {
		if _, err := (&PPS{PicInitQPMinus26: 26}).MarshalRBSP(sps); err == nil {
			t.Fatal("pic_init_qp_minus26=26 应返回错误")
		}
		if _, err := (&PPS{ChromaQPIndexOffset: 13}).MarshalRBSP(sps); err == nil {
			t.Fatal("chroma_qp_index_offset=13 应返回错误")
		}
	})
	t.Run("fmo missing run lengths", func(t *testing.T) {
		if _, err := (&PPS{NumSliceGroupsMinus1: 1}).MarshalRBSP(sps); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("scaling list missing", func(t *testing.T) {
		p := &PPS{PicScalingMatrixPresentFlag: true}
		p.PicScalingListPresentFlag[0] = true
		if _, err := p.MarshalRBSP(sps); err == nil {
			t.Fatal("缺少 scaling list 应返回错误")
		}
	})
	t.Run("scaling list wrong size", func(t *testing.T) {
		p := &PPS{PicScalingMatrixPresentFlag: true}
		p.PicScalingListPresentFlag[0] = true
		p.PicScalingList[0] = &ScalingList{Size: 8, Deltas: make([]int32, 16)}
		if _, err := p.MarshalRBSP(sps); err == nil {
			t.Fatal("scaling list 尺寸不匹配应返回错误")
		}
	})
}
