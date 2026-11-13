package syntax

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func encodeVUI(t *testing.T, v *VUI) []byte {
	t.Helper()
	w := bitstream.NewWriter()
	if err := v.WriteTo(w); err != nil {
		t.Fatalf("写出 VUI 失败: %v", err)
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		t.Fatalf("写出尾位失败: %v", err)
	}
	return w.Bytes()
}

func decodeVUI(t *testing.T, rbsp []byte) (*VUI, *bitstream.Reader) {
	t.Helper()
	r := bitstream.NewReader(rbsp)
	v, err := ParseVUI(r)
	if err != nil {
		t.Fatalf("解析 VUI 失败: %v", err)
	}
	return v, r
}

func fullVUI() *VUI {
	return &VUI{
		AspectRatioInfoPresentFlag: true,
		AspectRatioIDC:             255,
		SARWidth:                   16,
		SARHeight:                  9,

		OverscanInfoPresentFlag: true,
		OverscanAppropriateFlag: true,

		VideoSignalTypePresentFlag:   true,
		VideoFormat:                  5,
		VideoFullRangeFlag:           true,
		ColourDescriptionPresentFlag: true,
		ColourPrimaries:              1,
		TransferCharacteristics:      2,
		MatrixCoefficients:           3,

		ChromaLocInfoPresentFlag:       true,
		ChromaSampleLocTypeTopField:    4,
		ChromaSampleLocTypeBottomField: 5,

		TimingInfoPresentFlag: true,
		NumUnitsInTick:        1001,
		TimeScale:             30000,
		FixedFrameRateFlag:    true,

		NALHRDParametersPresentFlag: true,
		VCLHRDParametersPresentFlag: true,
		NALHRD: &HRDParameters{
			CPBCountMinus1: 1,
			BitRateScale:   2,
			CPBSizeScale:   3,
			CPB: []CPBEntry{
				{BitRateValueMinus1: 10, CPBSizeValueMinus1: 20, CBRFlag: true},
				{BitRateValueMinus1: 30, CPBSizeValueMinus1: 40, CBRFlag: false},
			},
			InitialCPBRemovalDelayLengthMinus1: 7,
			CPBRemovalDelayLengthMinus1:        8,
			DPBOutputDelayLengthMinus1:         9,
			TimeOffsetLength:                   10,
		},
		VCLHRD: &HRDParameters{
			CPBCountMinus1: 0,
			BitRateScale:   1,
			CPBSizeScale:   1,
			CPB: []CPBEntry{
				{BitRateValueMinus1: 1, CPBSizeValueMinus1: 2, CBRFlag: false},
			},
			InitialCPBRemovalDelayLengthMinus1: 1,
			CPBRemovalDelayLengthMinus1:        2,
			DPBOutputDelayLengthMinus1:         3,
			TimeOffsetLength:                   4,
		},
		LowDelayHRDFlag: true,

		PicStructPresentFlag: true,

		BitstreamRestrictionFlag:           true,
		MotionVectorsOverPicBoundariesFlag: true,
		MaxBytesPerPicDenom:                2,
		MaxBitsPerMBDenom:                  3,
		Log2MaxMVLengthHorizontal:          4,
		Log2MaxMVLengthVertical:            5,
		MaxNumReorderFrames:                6,
		MaxDecFrameBuffering:               7,
	}
}

func TestVUI_RoundTrip(t *testing.T) {
	cases := map[string]*VUI{
		"minimal": {},
		"full":    fullVUI(),
		"aspect non extended": {
			AspectRatioInfoPresentFlag: true,
			AspectRatioIDC:             1,
		},
		"video signal no colour": {
			VideoSignalTypePresentFlag: true,
			VideoFormat:                7,
		},
		"bitstream restriction": {
			BitstreamRestrictionFlag:           true,
			MotionVectorsOverPicBoundariesFlag: false,
			MaxBytesPerPicDenom:                16,
			MaxBitsPerMBDenom:                  16,
			Log2MaxMVLengthHorizontal:          15,
			Log2MaxMVLengthVertical:            15,
			MaxNumReorderFrames:                2,
			MaxDecFrameBuffering:               4,
		},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			rbsp := encodeVUI(t, v)
			got, r := decodeVUI(t, rbsp)
			if !reflect.DeepEqual(got, v) {
				t.Fatalf("解析结果不一致:\n got %+v\nwant %+v", got, v)
			}
			if more, err := moreRBSPData(r); err != nil || more {
				t.Fatalf("VUI 解析后仍有多余数据: more=%v err=%v", more, err)
			}

			w := bitstream.NewWriter()
			if err := got.WriteTo(w); err != nil {
				t.Fatalf("二次写出失败: %v", err)
			}
			if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
				t.Fatalf("写出尾位失败: %v", err)
			}
			if !bytes.Equal(w.Bytes(), rbsp) {
				t.Fatalf("往返字节不一致:\n got %x\nwant %x", w.Bytes(), rbsp)
			}
		})
	}
}

func TestParseVUI_Errors(t *testing.T) {
	t.Run("truncated", func(t *testing.T) {
		if _, err := ParseVUI(bitstream.NewReader(nil)); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("cpb count too large", func(t *testing.T) {
		w := bitstream.NewWriter()
		// 构造到 nal_hrd_parameters_present_flag：前面 5 个 false 标志，
		// 然后 nal_hrd 为 true，接着 cpb_cnt_minus1 = 32。
		for i := 0; i < 5; i++ {
			if err := w.WriteBit(0); err != nil {
				t.Fatalf("写标志失败: %v", err)
			}
		}
		if err := w.WriteBit(1); err != nil {
			t.Fatalf("写标志失败: %v", err)
		}
		if err := w.WriteUE(32); err != nil {
			t.Fatalf("写 cpb_cnt_minus1 失败: %v", err)
		}
		if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
			t.Fatalf("写尾位失败: %v", err)
		}
		if _, err := ParseVUI(bitstream.NewReader(w.Bytes())); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
}

func TestVUI_WriteErrors(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var v *VUI
		if err := v.WriteTo(bitstream.NewWriter()); err == nil {
			t.Fatal("nil VUI 应返回错误")
		}
	})
	t.Run("missing nal hrd", func(t *testing.T) {
		v := &VUI{NALHRDParametersPresentFlag: true}
		if err := v.WriteTo(bitstream.NewWriter()); err == nil {
			t.Fatal("缺少 NALHRD 应返回错误")
		}
	})
	t.Run("missing vcl hrd", func(t *testing.T) {
		v := &VUI{VCLHRDParametersPresentFlag: true}
		if err := v.WriteTo(bitstream.NewWriter()); err == nil {
			t.Fatal("缺少 VCLHRD 应返回错误")
		}
	})
	t.Run("cpb count mismatch", func(t *testing.T) {
		v := &VUI{
			NALHRDParametersPresentFlag: true,
			NALHRD:                      &HRDParameters{CPBCountMinus1: 1},
		}
		if err := v.WriteTo(bitstream.NewWriter()); err == nil {
			t.Fatal("CPB 数量不匹配应返回错误")
		}
	})
	t.Run("cpb count too large", func(t *testing.T) {
		v := &VUI{
			NALHRDParametersPresentFlag: true,
			NALHRD:                      &HRDParameters{CPBCountMinus1: 32, CPB: make([]CPBEntry, 33)},
		}
		if err := v.WriteTo(bitstream.NewWriter()); err == nil {
			t.Fatal("cpb_cnt_minus1 超限应返回错误")
		}
	})
}

// TestMoreRBSPData 验证尾位识别不会把数据位 0 误判为填充。
func TestMoreRBSPData(t *testing.T) {
	w := bitstream.NewWriter()
	if err := w.WriteBit(0); err != nil {
		t.Fatalf("写数据位失败: %v", err)
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		t.Fatalf("写尾位失败: %v", err)
	}
	r := bitstream.NewReader(w.Bytes())

	more, err := moreRBSPData(r)
	if err != nil || !more {
		t.Fatalf("读取数据位前 more=%v err=%v，期望 true", more, err)
	}
	if _, err := r.ReadBit(); err != nil {
		t.Fatalf("读取数据位失败: %v", err)
	}
	more, err = moreRBSPData(r)
	if err != nil || more {
		t.Fatalf("只剩尾位时 more=%v err=%v，期望 false", more, err)
	}
	if more, err := moreRBSPData(bitstream.NewReader(nil)); err != nil || more {
		t.Fatalf("空输入 more=%v err=%v", more, err)
	}
}
