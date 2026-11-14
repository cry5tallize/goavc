package syntax

import (
	"encoding/hex"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("解码十六进制失败: %v", err)
	}
	return b
}

// parseSPSHex 解析 NAL 字节（含 1 字节 header）形式的 SPS。
func parseSPSHex(t *testing.T, hexString string) *SPS {
	t.Helper()
	nal := decodeHex(t, hexString)
	rbsp, err := bitstream.EBSPToRBSP(nal[1:])
	if err != nil {
		t.Fatalf("去除防竞争字节失败: %v", err)
	}
	sps, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("解析 SPS 失败: %v", err)
	}
	return sps
}

// TestParseSPS_RealCorpus 使用 x264 生成的 Baseline/Main/High SPS 验证关键字段。
func TestParseSPS_RealCorpus(t *testing.T) {
	cases := []struct {
		name        string
		nalHex      string
		profile     uint8
		constraints [6]bool
	}{
		{
			name:        "baseline",
			nalHex:      "6742c00ad9047b011000000300100000030140f1226480",
			profile:     66,
			constraints: [6]bool{true, true, false, false, false, false},
		},
		{
			name:        "main",
			nalHex:      "674d400aeca23d8088000003000800000300a078912cb0",
			profile:     77,
			constraints: [6]bool{false, true, false, false, false, false},
		},
		{
			name:        "high",
			nalHex:      "6764000aacd9447b011000000300100000030140f1225960",
			profile:     100,
			constraints: [6]bool{false, false, false, false, false, false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sps := parseSPSHex(t, tc.nalHex)
			if sps.ProfileIDC != tc.profile {
				t.Fatalf("profile = %d，期望 %d", sps.ProfileIDC, tc.profile)
			}
			if sps.LevelIDC != 10 {
				t.Fatalf("level = %d，期望 10", sps.LevelIDC)
			}
			if sps.ConstraintSetFlags != tc.constraints {
				t.Fatalf("constraints = %v，期望 %v", sps.ConstraintSetFlags, tc.constraints)
			}
			if sps.ChromaFormatIDC != 1 {
				t.Fatalf("chroma_format_idc = %d，期望 1", sps.ChromaFormatIDC)
			}
			if !sps.FrameMbsOnlyFlag || sps.MbAdaptiveFrameFieldFlag {
				t.Fatalf("frame_mbs_only=%v mbaff=%v", sps.FrameMbsOnlyFlag, sps.MbAdaptiveFrameFieldFlag)
			}
			if sps.PicWidthInMbs() != 4 || sps.PicHeightInMapUnits() != 3 {
				t.Fatalf("宏块尺寸 = %dx%d", sps.PicWidthInMbs(), sps.PicHeightInMapUnits())
			}
			w, err := sps.DisplayWidth()
			if err != nil || w != 64 {
				t.Fatalf("DisplayWidth = %d, err=%v", w, err)
			}
			h, err := sps.DisplayHeight()
			if err != nil || h != 48 {
				t.Fatalf("DisplayHeight = %d, err=%v", h, err)
			}
			if !sps.VUIParametersPresentFlag || sps.VUI == nil {
				t.Fatalf("VUI 缺失: flag=%v", sps.VUIParametersPresentFlag)
			}
			if !sps.VUI.TimingInfoPresentFlag {
				t.Fatalf("VUI timing_info 缺失: %+v", sps.VUI)
			}
			if sps.VUI.TimeScale == 0 || sps.VUI.NumUnitsInTick == 0 {
				t.Fatalf("VUI timing 值异常: %+v", sps.VUI)
			}
		})
	}
}

// testSPS 描述合成 SPS 的字段，用于覆盖解析器的错误与边界分支。
type testSPS struct {
	profile     uint8
	chroma      uint32
	lumaDepth   uint32
	chromaDepth uint32
	qpprime     bool

	pocType         uint32
	frameMbsOnly    bool
	mbaff           bool
	cropping        bool
	cropLeft        uint32
	cropRight       uint32
	cropTop         uint32
	cropBottom      uint32
	widthMbsMinus1  uint32
	heightMapMinus1 uint32
	vui             *VUI
}

// 以下SPS字节直接按7.3.2.1.1拼接，POC类型固定为2，不使用SPS写出器。
// Level1的11x9 MB画面只能声明4个参考帧；第二项仅把ue(4)改为ue(5)。
func TestParseSPS_FixedLevelAndDPBContracts(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"level1 DPB boundary", []byte{0x42, 0x00, 0x0a, 0xd9, 0x42, 0xc4, 0xe4}, nil},
		{"implicit DPB too small", []byte{0x42, 0x00, 0x0a, 0xd9, 0x82, 0xc4, 0xe4}, h264.ErrInvalidData},
		{"unknown level", []byte{0x42, 0x00, 0x00, 0xda, 0x79}, h264.ErrUnsupported},
		{"PAFF level2.1 syntax", []byte{0x4d, 0x00, 0x15, 0xda, 0x24, 0x48}, nil},
		{"PAFF forbidden at level1", []byte{0x4d, 0x00, 0x0a, 0xda, 0x24, 0x48}, h264.ErrInvalidData},
		{"level3 direct inference", []byte{0x4d, 0x00, 0x1e, 0xda, 0x25, 0x10}, h264.ErrInvalidData},
		{"legacy level1b", []byte{0x4d, 0x10, 0x0b, 0xda, 0x0b, 0x13, 0x90}, nil},
		{"legacy level1.1", []byte{0x4d, 0x00, 0x0b, 0xda, 0x05, 0x82, 0x59}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSPS(tc.data)
			if (tc.want == nil && err != nil) || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("解析固定SPS = %+v, %v，期望%v", s, err, tc.want)
			}
		})
	}
}

func writeOrFail(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("构造测试 SPS 失败: %v", err)
	}
}

func encodeTestSPS(t *testing.T, p testSPS) []byte {
	t.Helper()
	if p.chroma == 0 && !usesHighProfileSyntax(p.profile) {
		p.chroma = 1
	}
	w := bitstream.NewWriter()
	writeOrFail(t, w.WriteBits(8, uint64(p.profile)))
	writeOrFail(t, w.WriteBits(8, 0)) // constraint flags + reserved
	writeOrFail(t, w.WriteBits(8, 10))
	writeOrFail(t, w.WriteUE(0)) // sps id

	if usesHighProfileSyntax(p.profile) {
		writeOrFail(t, w.WriteUE(p.chroma))
		if p.chroma == 3 {
			writeOrFail(t, w.WriteBit(0)) // separate_colour_plane_flag
		}
		writeOrFail(t, w.WriteUE(p.lumaDepth))
		writeOrFail(t, w.WriteUE(p.chromaDepth))
		writeOrFail(t, w.WriteBit(boolBit(p.qpprime)))
		writeOrFail(t, w.WriteBit(0)) // seq_scaling_matrix_present
	}

	writeOrFail(t, w.WriteUE(0)) // log2_max_frame_num_minus4
	writeOrFail(t, w.WriteUE(p.pocType))
	if p.pocType == 0 {
		writeOrFail(t, w.WriteUE(0)) // log2_max_pic_order_cnt_lsb_minus4
	}
	if p.pocType == 1 {
		writeOrFail(t, w.WriteBit(0)) // delta_pic_order_always_zero
		writeOrFail(t, w.WriteSE(0))  // offset_for_non_ref_pic
		writeOrFail(t, w.WriteSE(0))  // offset_for_top_to_bottom_field
		writeOrFail(t, w.WriteUE(2))  // num_ref_frames_in_pic_order_cnt_cycle
		writeOrFail(t, w.WriteSE(1))
		writeOrFail(t, w.WriteSE(-1))
	}
	writeOrFail(t, w.WriteUE(1))  // max_num_ref_frames
	writeOrFail(t, w.WriteBit(0)) // gaps_in_frame_num_value_allowed

	writeOrFail(t, w.WriteUE(p.widthMbsMinus1))
	writeOrFail(t, w.WriteUE(p.heightMapMinus1))
	if p.frameMbsOnly {
		writeOrFail(t, w.WriteBit(1))
	} else {
		writeOrFail(t, w.WriteBit(0))
		writeOrFail(t, w.WriteBit(boolBit(p.mbaff)))
	}
	writeOrFail(t, w.WriteBit(1)) // direct_8x8_inference

	writeOrFail(t, w.WriteBit(boolBit(p.cropping)))
	if p.cropping {
		writeOrFail(t, w.WriteUE(p.cropLeft))
		writeOrFail(t, w.WriteUE(p.cropRight))
		writeOrFail(t, w.WriteUE(p.cropTop))
		writeOrFail(t, w.WriteUE(p.cropBottom))
	}
	writeOrFail(t, w.WriteBit(boolBit(p.vui != nil)))
	if p.vui != nil {
		writeOrFail(t, p.vui.WriteTo(w))
	}
	writeOrFail(t, bitstream.WriteRBSPTrailingBits(w))
	return w.Bytes()
}

func boolBit(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func TestParseSPS_SyntheticBaseline(t *testing.T) {
	rbsp := encodeTestSPS(t, testSPS{
		profile:         66,
		frameMbsOnly:    true,
		widthMbsMinus1:  3,
		heightMapMinus1: 2,
		cropLeft:        1,
		cropRight:       0,
		cropTop:         1,
		cropBottom:      0,
		cropping:        true,
	})
	sps, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if sps.PicOrderCntType != 0 || sps.ProfileIDC != 66 {
		t.Fatalf("字段 = %+v", sps)
	}
	w, err := sps.DisplayWidth()
	if err != nil || w != 62 {
		t.Fatalf("DisplayWidth = %d, err=%v", w, err)
	}
	h, err := sps.DisplayHeight()
	if err != nil || h != 46 {
		t.Fatalf("DisplayHeight = %d, err=%v", h, err)
	}
}

func TestParseSPS_POCType1(t *testing.T) {
	rbsp := encodeTestSPS(t, testSPS{
		profile:         77,
		pocType:         1,
		frameMbsOnly:    true,
		widthMbsMinus1:  3,
		heightMapMinus1: 2,
	})
	sps, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if sps.PicOrderCntType != 1 {
		t.Fatalf("poc type = %d", sps.PicOrderCntType)
	}
	if sps.NumRefFramesInPicOrderCntCycle != 2 ||
		len(sps.OffsetForRefFrame) != 2 ||
		sps.OffsetForRefFrame[0] != 1 ||
		sps.OffsetForRefFrame[1] != -1 {
		t.Fatalf("POC offsets = %+v", sps.OffsetForRefFrame)
	}
}

func TestParseSPS_Unsupported(t *testing.T) {
	tests := map[string]testSPS{
		"unsupported profile": {
			profile: 44, chroma: 1, frameMbsOnly: true,
			widthMbsMinus1: 3, heightMapMinus1: 2,
		},
		"chroma 422": {
			profile: 100, chroma: 2, frameMbsOnly: true,
			widthMbsMinus1: 3, heightMapMinus1: 2,
		},
		"high10": {
			profile: 100, chroma: 1, lumaDepth: 2, chromaDepth: 2, frameMbsOnly: true,
			widthMbsMinus1: 3, heightMapMinus1: 2,
		},
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			rbsp := encodeTestSPS(t, p)
			if _, err := ParseSPS(rbsp); !errors.Is(err, h264.ErrUnsupported) {
				t.Fatalf("错误 = %v，期望 ErrUnsupported", err)
			}
		})
	}
}

func TestParseSPS_Invalid(t *testing.T) {
	t.Run("reserved bits", func(t *testing.T) {
		if _, err := ParseSPS([]byte{0x42, 0x01}); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("poc type 3", func(t *testing.T) {
		rbsp := encodeTestSPS(t, testSPS{
			profile:         66,
			pocType:         3,
			frameMbsOnly:    true,
			widthMbsMinus1:  3,
			heightMapMinus1: 2,
		})
		if _, err := ParseSPS(rbsp); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		if _, err := ParseSPS(nil); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
}

func TestParseSPS_CropOverflow(t *testing.T) {
	rbsp := encodeTestSPS(t, testSPS{
		profile:         66,
		frameMbsOnly:    true,
		widthMbsMinus1:  3,
		heightMapMinus1: 2,
		cropping:        true,
		cropLeft:        100,
	})
	sps, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if _, err := sps.DisplayWidth(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
	}
}

// VUI独立语法合法但与SPS上下文冲突，必须由完整SPS解析入口拒绝。
func TestParseSPS_EmbeddedVUIContextContracts(t *testing.T) {
	for _, tc := range []struct {
		name string
		vui  *VUI
	}{
		{"DPB smaller than reference set", &VUI{BitstreamRestrictionFlag: true}},
		{"DPB exceeds level", &VUI{BitstreamRestrictionFlag: true, MaxDecFrameBuffering: 5}},
		{"NAL HRD exceeds level", &VUI{NALHRDParametersPresentFlag: true, NALHRD: &HRDParameters{CPB: []CPBEntry{{BitRateValueMinus1: 1200}}}}},
		{"VCL HRD exceeds level", &VUI{VCLHRDParametersPresentFlag: true, VCLHRD: &HRDParameters{CPB: []CPBEntry{{BitRateValueMinus1: 1000}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := encodeTestSPS(t, testSPS{
				profile: 66, frameMbsOnly: true, widthMbsMinus1: 10, heightMapMinus1: 8,
				vui: tc.vui,
			})
			if _, err := ParseSPS(data); !errors.Is(err, h264.ErrInvalidData) || errors.Is(err, h264.ErrResourceLimit) {
				t.Fatalf("SPS上下文违规必须是语义错误: %v", err)
			}
		})
	}
}
