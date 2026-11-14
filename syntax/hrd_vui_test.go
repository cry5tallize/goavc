package syntax

import (
	"bytes"
	"errors"
	"math"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

func hrdVUIForTest() *VUI {
	return &VUI{
		NALHRDParametersPresentFlag: true,
		NALHRD:                      &HRDParameters{CPB: []CPBEntry{{}}},
	}
}

func TestHRDVUIConditionalValidation(t *testing.T) {
	cases := []struct {
		name string
		edit func(*VUI)
	}{
		{"missing nal", func(v *VUI) { v.NALHRD = nil }},
		{"missing vcl", func(v *VUI) { v.VCLHRDParametersPresentFlag = true }},
		{"nonprime SAR", func(v *VUI) {
			v.AspectRatioInfoPresentFlag, v.AspectRatioIDC, v.SARWidth, v.SARHeight = true, 255, 32, 18
		}},
		{"video width", func(v *VUI) { v.VideoSignalTypePresentFlag, v.VideoFormat = true, 8 }},
		{"chroma top", func(v *VUI) { v.ChromaLocInfoPresentFlag, v.ChromaSampleLocTypeTopField = true, 6 }},
		{"chroma bottom", func(v *VUI) { v.ChromaLocInfoPresentFlag, v.ChromaSampleLocTypeBottomField = true, 6 }},
		{"zero tick", func(v *VUI) { v.TimingInfoPresentFlag, v.TimeScale = true, 1 }},
		{"zero scale", func(v *VUI) { v.TimingInfoPresentFlag, v.NumUnitsInTick = true, 1 }},
		{"low delay fixed timing", func(v *VUI) {
			v.TimingInfoPresentFlag, v.NumUnitsInTick, v.TimeScale = true, 1, 50
			v.FixedFrameRateFlag, v.LowDelayHRDFlag = true, true
		}},
		{"low delay multiple nal CPBs", func(v *VUI) {
			v.LowDelayHRDFlag = true
			v.NALHRD.CPBCountMinus1 = 1
			v.NALHRD.CPB = []CPBEntry{{}, {BitRateValueMinus1: 1}}
		}},
		{"low delay multiple vcl CPBs", func(v *VUI) {
			v.NALHRDParametersPresentFlag = false
			v.VCLHRDParametersPresentFlag, v.LowDelayHRDFlag = true, true
			v.VCLHRD = &HRDParameters{CPBCountMinus1: 1, CPB: []CPBEntry{{}, {BitRateValueMinus1: 1}}}
		}},
		{"byte denominator", func(v *VUI) { v.BitstreamRestrictionFlag, v.MaxBytesPerPicDenom = true, 17 }},
		{"bit denominator", func(v *VUI) { v.BitstreamRestrictionFlag, v.MaxBitsPerMBDenom = true, 17 }},
		{"horizontal MV", func(v *VUI) { v.BitstreamRestrictionFlag, v.Log2MaxMVLengthHorizontal = true, 16 }},
		{"vertical MV", func(v *VUI) { v.BitstreamRestrictionFlag, v.Log2MaxMVLengthVertical = true, 16 }},
		{"reorder", func(v *VUI) { v.BitstreamRestrictionFlag, v.MaxNumReorderFrames = true, 1 }},
		{"DPB", func(v *VUI) { v.BitstreamRestrictionFlag, v.MaxDecFrameBuffering = true, 17 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := hrdVUIForTest()
			tc.edit(v)
			if err := v.Validate(); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("Validate: %v", err)
			}
			w := bitstream.NewWriter()
			if err := w.WriteBits(3, 5); err != nil {
				t.Fatal(err)
			}
			before := w.Bytes()
			if err := v.WriteTo(w); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("WriteTo: %v", err)
			}
			if w.BitsWritten() != 3 || !bytes.Equal(w.Bytes(), before) {
				t.Fatal("无效 VUI 在预验证前修改了写入器")
			}
		})
	}
}

func TestHRDVUIDualDelayLengths(t *testing.T) {
	changes := []func(*HRDParameters){
		func(h *HRDParameters) { h.InitialCPBRemovalDelayLengthMinus1 = 1 },
		func(h *HRDParameters) { h.CPBRemovalDelayLengthMinus1 = 1 },
		func(h *HRDParameters) { h.DPBOutputDelayLengthMinus1 = 1 },
		func(h *HRDParameters) { h.TimeOffsetLength = 1 },
	}
	for i, change := range changes {
		v := hrdVUIForTest()
		v.VCLHRDParametersPresentFlag = true
		v.VCLHRD = &HRDParameters{CPB: []CPBEntry{{}}}
		change(v.VCLHRD)
		if err := v.Validate(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("双 HRD 第 %d 个延时字段不一致: %v", i, err)
		}
	}
	// 规范只要求四种延时长度相等，不要求两个 HRD 的尺度或 CPB 数量相等。
	v := hrdVUIForTest()
	v.NALHRD.CPBCountMinus1 = 1
	v.NALHRD.CPB = []CPBEntry{{CPBSizeValueMinus1: 1}, {BitRateValueMinus1: 1}}
	v.VCLHRDParametersPresentFlag = true
	v.VCLHRD = &HRDParameters{BitRateScale: 15, CPBSizeScale: 15, CPB: []CPBEntry{{}}}
	if err := v.Validate(); err != nil {
		t.Fatalf("合法的不同 NAL/VCL 调度方案: %v", err)
	}
}

func TestHRDVUIAbsentFieldsAreNotValues(t *testing.T) {
	v := &VUI{
		AspectRatioIDC: 255, SARWidth: 32, SARHeight: 18,
		VideoFormat: 255, ColourDescriptionPresentFlag: true,
		ColourPrimaries: 0, TransferCharacteristics: 0, MatrixCoefficients: 0,
		ChromaSampleLocTypeTopField:    math.MaxUint32,
		ChromaSampleLocTypeBottomField: math.MaxUint32,
		FixedFrameRateFlag:             true, LowDelayHRDFlag: true,
		NALHRD:              &HRDParameters{CPBCountMinus1: math.MaxUint32},
		VCLHRD:              &HRDParameters{BitRateScale: 255},
		MaxBytesPerPicDenom: math.MaxUint32, MaxBitsPerMBDenom: math.MaxUint32,
		Log2MaxMVLengthHorizontal: math.MaxUint32, Log2MaxMVLengthVertical: math.MaxUint32,
		MaxNumReorderFrames: math.MaxUint32, MaxDecFrameBuffering: math.MaxUint32,
	}
	if err := v.Validate(); err != nil {
		t.Fatalf("不存在字段不应参与校验: %v", err)
	}
	w := bitstream.NewWriter()
	if err := v.WriteTo(w); err != nil || w.BitsWritten() != 9 || !bytes.Equal(w.Bytes(), []byte{0, 0}) {
		t.Fatalf("不存在字段被错误写出: %x/%d, %v", w.Bytes(), w.BitsWritten(), err)
	}
	// 只有实际出现的 fixed_frame_rate_flag 才约束 low_delay_hrd_flag。
	v = hrdVUIForTest()
	v.LowDelayHRDFlag, v.FixedFrameRateFlag = true, true
	if err := v.Validate(); err != nil {
		t.Fatalf("缺省 fixed_frame_rate_flag 不应从非生效字段读取: %v", err)
	}
	// 没有 HRD 时 low_delay 字段不出现，不应拒绝保留在结构中的 true。
	v = &VUI{TimingInfoPresentFlag: true, NumUnitsInTick: 1, TimeScale: 50, FixedFrameRateFlag: true, LowDelayHRDFlag: true}
	if err := v.Validate(); err != nil {
		t.Fatalf("缺省 low_delay_hrd_flag 不应从非生效字段读取: %v", err)
	}
}

func TestHRDVUIAcceptsSemanticEndpoints(t *testing.T) {
	for _, sar := range [][2]uint16{{0, 0}, {0, 9}, {16, 0}, {1, 1}, {16, 9}, {65535, 65534}} {
		v := &VUI{AspectRatioInfoPresentFlag: true, AspectRatioIDC: 255, SARWidth: sar[0], SARHeight: sar[1]}
		if err := v.Validate(); err != nil {
			t.Fatalf("SAR %v: %v", sar, err)
		}
	}
	for _, idc := range []uint8{0, 1, 16, 17, 254} {
		if err := (&VUI{AspectRatioInfoPresentFlag: true, AspectRatioIDC: idc}).Validate(); err != nil {
			t.Fatalf("aspect_ratio_idc=%d: %v", idc, err)
		}
	}
	for _, colour := range []uint8{0, 1, 2, 3, 9, 16, 18, 255} {
		v := &VUI{VideoSignalTypePresentFlag: true, VideoFormat: 7, ColourDescriptionPresentFlag: true,
			ColourPrimaries: colour, TransferCharacteristics: colour, MatrixCoefficients: colour}
		if err := v.Validate(); err != nil {
			t.Fatalf("colour 保留值 %d: %v", colour, err)
		}
	}
	v := &VUI{
		ChromaLocInfoPresentFlag: true, ChromaSampleLocTypeTopField: 0, ChromaSampleLocTypeBottomField: 5,
		TimingInfoPresentFlag: true, NumUnitsInTick: math.MaxUint32, TimeScale: math.MaxUint32,
		BitstreamRestrictionFlag: true, MaxBytesPerPicDenom: 16, MaxBitsPerMBDenom: 16,
		Log2MaxMVLengthHorizontal: 15, Log2MaxMVLengthVertical: 15,
		MaxNumReorderFrames: 16, MaxDecFrameBuffering: 16,
	}
	if err := v.Validate(); err != nil {
		t.Fatalf("合法字段上界: %v", err)
	}
	var nilVUI *VUI
	if err := nilVUI.Validate(); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil VUI: %v", err)
	}
	if _, err := ParseVUI(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil VUI reader: %v", err)
	}
	if err := (&VUI{}).WriteTo(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil VUI writer: %v", err)
	}
}

func TestHRDVUIParseInvalidIndependentVectors(t *testing.T) {
	cases := []struct {
		name string
		bits string
	}{
		{"nonprime SAR", "1 11111111 0000000000100000 0000000000010010 00000000"},
		{"chroma top six", "0001 00111 1 00000"},
		{"chroma bottom six", "0001 1 00111 00000"},
		{"zero tick", "00001 00000000000000000000000000000000 00000000000000000000000000000001 0 0000"},
		{"zero time scale", "00001 00000000000000000000000000000001 00000000000000000000000000000000 0 0000"},
		{"denominator seventeen", "00000000 1 0 000010010 1 1 1 1 1"},
		{"horizontal MV sixteen", "00000000 1 0 1 1 000010001 1 1 1"},
		{"vertical MV sixteen", "00000000 1 0 1 1 1 000010001 1 1"},
		{"reorder exceeds DPB", "00000000 1 0 1 1 1 1 010 1"},
		{"DPB seventeen", "00000000 1 0 1 1 1 1 1 000010010"},
		// num_units_in_tick=1、time_scale=50、fixed=1，NAL HRD 后 low_delay=1。
		{"fixed and low delay", "00001 00000000000000000000000000000001 00000000000000000000000000110010 1 1 10000000 01110000 00000000 00000000 0 1 0 0"},
		// 两个 HRD 的 time_offset_length 分别为 0 和 1。
		{"dual delay mismatch", "00000 1 10000000 01110000 00000000 00000000 1 10000000 01110000 00000000 00000001 0 0 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseVUI(bitstream.NewReader(hrdBitsForTest(t, tc.bits))); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("非法语义未被拒绝: %v", err)
			}
		})
	}
}

func TestHRDVUITruncationAtEveryByte(t *testing.T) {
	bits := "00001 00000000000000000000000000000001 00000000000000000000000000110010 0 1 10000000 01110000 00000000 00000000 1 10000000 01110000 00000000 00000000 0 0 0"
	data := hrdBitsForTest(t, bits)
	if _, err := ParseVUI(bitstream.NewReader(data)); err != nil {
		t.Fatalf("完整双 HRD 向量: %v", err)
	}
	for n := range len(data) {
		if _, err := ParseVUI(bitstream.NewReader(data[:n])); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("双 HRD 向量截断为 %d 字节: %v", n, err)
		}
	}
}
