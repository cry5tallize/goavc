package syntax

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/goavc/h264/bitstream"
)

func spsRBSPFromHex(t *testing.T, hexString string) []byte {
	t.Helper()
	nal := decodeHex(t, hexString)
	rbsp, err := bitstream.EBSPToRBSP(nal[1:])
	if err != nil {
		t.Fatalf("去除防竞争字节失败: %v", err)
	}
	return rbsp
}

// TestSPS_MarshalRBSP_RealCorpus 验证真实 x264 SPS 的逐字节往返。
func TestSPS_MarshalRBSP_RealCorpus(t *testing.T) {
	cases := map[string]string{
		"baseline": "6742c00ad9047b011000000300100000030140f1226480",
		"main":     "674d400aeca23d8088000003000800000300a078912cb0",
		"high":     "6764000aacd9447b011000000300100000030140f1225960",
	}
	for name, nalHex := range cases {
		t.Run(name, func(t *testing.T) {
			original := spsRBSPFromHex(t, nalHex)
			sps, err := ParseSPS(original)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			got, err := sps.MarshalRBSP()
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("往返字节不一致:\n got %x\nwant %x", got, original)
			}
		})
	}
}

// TestSPS_RoundTrip_Synthetic 覆盖合成 Baseline 与 POC type 1。
func TestSPS_RoundTrip_Synthetic(t *testing.T) {
	cases := map[string]testSPS{
		"baseline": {
			profile:         66,
			frameMbsOnly:    true,
			widthMbsMinus1:  3,
			heightMapMinus1: 2,
		},
		"main poc1": {
			profile:         77,
			pocType:         1,
			frameMbsOnly:    true,
			widthMbsMinus1:  7,
			heightMapMinus1: 5,
		},
		"cropped": {
			profile:         66,
			frameMbsOnly:    true,
			widthMbsMinus1:  3,
			heightMapMinus1: 2,
			cropping:        true,
			cropLeft:        1,
			cropRight:       1,
			cropTop:         1,
			cropBottom:      1,
		},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			rbsp := encodeTestSPS(t, p)
			sps, err := ParseSPS(rbsp)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			got, err := sps.MarshalRBSP()
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			if !bytes.Equal(got, rbsp) {
				t.Fatalf("往返字节不一致:\n got %x\nwant %x", got, rbsp)
			}
		})
	}
}

// TestSPS_RoundTrip_VUI 验证 SPS 内嵌 VUI 的往返。
func TestSPS_RoundTrip_VUI(t *testing.T) {
	sps, err := ParseSPS(encodeTestSPS(t, testSPS{
		profile:         66,
		frameMbsOnly:    true,
		widthMbsMinus1:  3,
		heightMapMinus1: 2,
	}))
	if err != nil {
		t.Fatalf("基础 SPS 解析失败: %v", err)
	}
	sps.VUIParametersPresentFlag = true
	sps.VUI = &VUI{
		TimingInfoPresentFlag:              true,
		NumUnitsInTick:                     1001,
		TimeScale:                          30000,
		FixedFrameRateFlag:                 true,
		BitstreamRestrictionFlag:           true,
		MotionVectorsOverPicBoundariesFlag: true,
		MaxBytesPerPicDenom:                2,
		MaxBitsPerMBDenom:                  2,
		Log2MaxMVLengthHorizontal:          15,
		Log2MaxMVLengthVertical:            15,
		MaxNumReorderFrames:                2,
		MaxDecFrameBuffering:               4,
	}
	rbsp, err := sps.MarshalRBSP()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	parsed, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("二次解析失败: %v", err)
	}
	if !reflect.DeepEqual(parsed.VUI, sps.VUI) {
		t.Fatalf("VUI 不一致:\n got %+v\nwant %+v", parsed.VUI, sps.VUI)
	}
}

// TestSPS_RoundTrip_ScalingMatrix 验证 High profile 缩放矩阵的往返。
func TestSPS_RoundTrip_ScalingMatrix(t *testing.T) {
	sps := &SPS{
		ProfileIDC:                  100,
		LevelIDC:                    31,
		SeqParameterSetID:           0,
		ChromaFormatIDC:             1,
		Log2MaxFrameNumMinus4:       0,
		PicOrderCntType:             0,
		MaxNumRefFrames:             1,
		PicWidthInMbsMinus1:         3,
		PicHeightInMapUnitsMinus1:   2,
		FrameMbsOnlyFlag:            true,
		Direct8x8InferenceFlag:      true,
		SeqScalingMatrixPresentFlag: true,
	}
	sps.SeqScalingListPresentFlag[0] = true
	sps.SeqScalingListPresentFlag[1] = true
	sps.SeqScalingList[0] = &ScalingList{Size: 4, Deltas: []int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}}
	sps.SeqScalingList[1] = &ScalingList{Size: 4, Deltas: []int32{-8}}

	rbsp, err := sps.MarshalRBSP()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	parsed, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !parsed.SeqScalingListPresentFlag[0] || !parsed.SeqScalingListPresentFlag[1] {
		t.Fatalf("scaling list present flags = %v", parsed.SeqScalingListPresentFlag)
	}
	for i := 0; i < 2; i++ {
		got, want := parsed.SeqScalingList[i], sps.SeqScalingList[i]
		if got.Size != want.Size || !reflect.DeepEqual(got.Deltas, want.Deltas) {
			t.Fatalf("scaling list %d 不一致:\n got size=%d deltas=%v\nwant size=%d deltas=%v",
				i, got.Size, got.Deltas, want.Size, want.Deltas)
		}
		if len(got.Values) != got.Size*got.Size {
			t.Fatalf("scaling list %d Values 长度 = %d", i, len(got.Values))
		}
	}

	again, err := parsed.MarshalRBSP()
	if err != nil {
		t.Fatalf("二次序列化失败: %v", err)
	}
	if !bytes.Equal(again, rbsp) {
		t.Fatalf("二次往返字节不一致")
	}
}

// TestSPS_WriteErrors 验证非法配置在写出前被拒绝。
func TestSPS_WriteErrors(t *testing.T) {
	valid := func() *SPS {
		return &SPS{
			ProfileIDC:                66,
			LevelIDC:                  10,
			MaxNumRefFrames:           1,
			PicWidthInMbsMinus1:       3,
			PicHeightInMapUnitsMinus1: 2,
			FrameMbsOnlyFlag:          true,
			Direct8x8InferenceFlag:    true,
		}
	}
	tests := map[string]func() *SPS{
		"nil": func() *SPS { return nil },
		"reserved bits": func() *SPS {
			s := valid()
			s.ReservedZero2Bits = 1
			return s
		},
		"sps id too large": func() *SPS {
			s := valid()
			s.SeqParameterSetID = 32
			return s
		},
		"poc type 3": func() *SPS {
			s := valid()
			s.PicOrderCntType = 3
			return s
		},
		"poc cycle mismatch": func() *SPS {
			s := valid()
			s.PicOrderCntType = 1
			s.NumRefFramesInPicOrderCntCycle = 2
			return s
		},
		"vui missing": func() *SPS {
			s := valid()
			s.VUIParametersPresentFlag = true
			return s
		},
		"scaling list missing": func() *SPS {
			s := valid()
			s.ProfileIDC = 100
			s.ChromaFormatIDC = 1
			s.SeqScalingMatrixPresentFlag = true
			s.SeqScalingListPresentFlag[0] = true
			return s
		},
		"scaling list wrong size": func() *SPS {
			s := valid()
			s.ProfileIDC = 100
			s.ChromaFormatIDC = 1
			s.SeqScalingMatrixPresentFlag = true
			s.SeqScalingListPresentFlag[0] = true
			s.SeqScalingList[0] = &ScalingList{Size: 8, Deltas: []int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}}
			return s
		},
		"unsupported profile": func() *SPS {
			s := valid()
			s.ProfileIDC = 122
			s.ChromaFormatIDC = 1
			return s
		},
		"high10": func() *SPS {
			s := valid()
			s.ProfileIDC = 100
			s.ChromaFormatIDC = 1
			s.BitDepthLumaMinus8 = 2
			s.BitDepthChromaMinus8 = 2
			return s
		},
	}
	for name, makeSPS := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := makeSPS().MarshalRBSP(); err == nil {
				t.Fatal("期望返回错误")
			}
		})
	}
}

// TestSPS_MarshalRBSP_TrailingBits 验证序列化结果可被再次完整解析。
func TestSPS_MarshalRBSP_TrailingBits(t *testing.T) {
	rbsp := encodeTestSPS(t, testSPS{
		profile:         66,
		frameMbsOnly:    true,
		widthMbsMinus1:  3,
		heightMapMinus1: 2,
	})
	sps, err := ParseSPS(rbsp)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out, err := sps.MarshalRBSP()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := bitstream.ValidateRBSPTrailingBits(out); err != nil {
		t.Fatalf("尾位非法: %v", err)
	}
	if _, err := ParseSPS(out); err != nil {
		t.Fatalf("二次解析失败: %v", err)
	}
}
