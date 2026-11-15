package syntax

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// 固定字节按 E.1.1/E.1.2 字段顺序独立拼接，不调用 VUI/HRD 写出器生成。
// 101 前缀后：Extended_SAR=16/9、overscan=1、format=5/full-range=1、
// colour=1/2/3、chroma_loc=4/5、timing=1001/30000/fixed=1；
// NAL HRD 为两个调度项（scale=2/3），VCL HRD 为一个调度项（scale=1/1）；
// 四个长度为7/8/9/10，low_delay=0、pic_struct=1、restriction=1，
// 最后七项为1、ue(2)、ue(3)、ue(4)、ue(5)、ue(6)、ue(7)。VUI共291位。
func TestVUIFullFixedBytesUnaligned(t *testing.T) {
	data := []byte{
		0xbf, 0xf0, 0x01, 0x00, 0x00, 0x9f, 0x70, 0x10, 0x20, 0x39,
		0x4d, 0x00, 0x00, 0x03, 0xe9, 0x00, 0x00, 0x75, 0x30, 0xd1,
		0x18, 0xb0, 0x53, 0x0f, 0x85, 0x47, 0x42, 0x55, 0x88, 0xa6,
		0x3a, 0x12, 0xa7, 0x64, 0x29, 0x8e, 0x22, 0xa0,
	}
	r := bitstream.NewReader(data)
	if prefix, err := r.ReadBits(3); err != nil || prefix != 5 {
		t.Fatalf("前缀 = %d, %v", prefix, err)
	}
	got, err := ParseVUI(r)
	if err != nil || r.BitsRead() != 294 || !reflect.DeepEqual(got, fullVUI()) {
		t.Fatalf("固定字节解析 = %+v/%d, %v", got, r.BitsRead(), err)
	}
	if suffix, err := r.ReadBits(5); err != nil || suffix != 21 {
		t.Fatalf("VUI后续字段 = %d, %v", suffix, err)
	}
	w := bitstream.NewWriter()
	if err := w.WriteBits(3, 5); err != nil {
		t.Fatal(err)
	}
	if err := fullVUI().WriteTo(w); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteBits(5, 21); err != nil {
		t.Fatal(err)
	}
	if w.BitsWritten() != 299 || !bytes.Equal(w.Bytes(), data) {
		t.Fatalf("固定字节生成 = %x/%d", w.Bytes(), w.BitsWritten())
	}
	// 前294位才是完整VUI；第38字节只有后续字段，不能误判为VUI必需数据。
	for n := 1; n < 37; n++ {
		r := bitstream.NewReader(data[:n])
		if _, err := r.ReadBits(3); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseVUI(r); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("截断到%d字节 = %v", n, err)
		}
	}
}

func vuiLevelSPS(profile, level uint8) *SPS {
	return &SPS{
		ProfileIDC: profile, LevelIDC: level, ChromaFormatIDC: 1,
		FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true,
	}
}

func vuiLevelHRD(rate, size uint64) *HRDParameters {
	return &HRDParameters{CPB: []CPBEntry{{
		BitRateValueMinus1: uint32(rate/64 - 1),
		CPBSizeValueMinus1: uint32(size/16 - 1),
	}}}
}

// 表 A-1 的全部level；HRD整数粒度边界按规范固定表独立给出。
func TestVUIAnnexALevelHRDBounds(t *testing.T) {
	for _, tc := range []struct {
		level         uint8
		maxBR, maxCPB uint64
	}{
		{9, 128, 350}, {10, 64, 175}, {11, 192, 500},
		{12, 384, 1000}, {13, 768, 2000}, {20, 2000, 2000},
		{21, 4000, 4000}, {22, 4000, 4000}, {30, 10000, 10000},
		{31, 14000, 14000}, {32, 20000, 20000}, {40, 20000, 25000},
		{41, 50000, 62500}, {42, 50000, 62500}, {50, 135000, 135000},
		{51, 240000, 240000}, {52, 240000, 240000}, {60, 240000, 240000},
		{61, 480000, 480000}, {62, 800000, 800000},
	} {
		s := vuiLevelSPS(100, tc.level)
		maxRate, maxSize := tc.maxBR*1250, tc.maxCPB*1250
		h := vuiLevelHRD(maxRate/64*64, maxSize/16*16)
		v := &VUI{VCLHRDParametersPresentFlag: true, VCLHRD: h}
		if err := v.ValidateForSPS(s); err != nil {
			t.Fatalf("level%d精确粒度内边界: %v", tc.level, err)
		}
		h.CPB[0].BitRateValueMinus1++
		if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) || errors.Is(err, h264.ErrResourceLimit) {
			t.Fatalf("level%d超码率: %v", tc.level, err)
		}
		h.CPB[0].BitRateValueMinus1--
		h.CPB[0].CPBSizeValueMinus1++
		if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("level%d超CPB: %v", tc.level, err)
		}
	}
}

func TestVUIAnnexAProfileFactors(t *testing.T) {
	for _, tc := range []struct {
		profile  uint8
		vcl, nal uint64
	}{
		{66, 1000, 1200}, {77, 1000, 1200}, {88, 1000, 1200},
		{100, 1250, 1500}, {110, 3000, 3600}, {122, 4000, 4800}, {244, 4000, 4800},
	} {
		for _, nal := range []bool{false, true} {
			factor := tc.vcl
			if nal {
				factor = tc.nal
			}
			h := vuiLevelHRD(64*factor, 175*factor/16*16)
			v := &VUI{VCLHRDParametersPresentFlag: !nal, VCLHRD: h, NALHRDParametersPresentFlag: nal, NALHRD: h}
			s := vuiLevelSPS(tc.profile, 10)
			if err := v.ValidateForSPS(s); err != nil {
				t.Fatalf("profile%d NAL=%v边界: %v", tc.profile, nal, err)
			}
			h.CPB[0].BitRateValueMinus1++
			if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("profile%d NAL=%v倍率码率超界: %v", tc.profile, nal, err)
			}
			h.CPB[0].BitRateValueMinus1--
			h.CPB[0].CPBSizeValueMinus1++
			if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("profile%d NAL=%v倍率CPB超界: %v", tc.profile, nal, err)
			}
		}
	}
}

func TestVUIAnnexAAlternativeSchedules(t *testing.T) {
	s := vuiLevelSPS(77, 10)
	// Level1 VCL上限64000/175000；两个方案各自只满足其中一项，整体不合规。
	h := &HRDParameters{CPBCountMinus1: 1, CPB: []CPBEntry{
		{BitRateValueMinus1: 999, CPBSizeValueMinus1: 10937},
		{BitRateValueMinus1: 1000, CPBSizeValueMinus1: 10936},
	}}
	v := &VUI{VCLHRDParametersPresentFlag: true, VCLHRD: h}
	if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("必须由同一方案同时满足两项level上限: %v", err)
	}
	h.CPB[1].BitRateValueMinus1 = 999
	h.CPB[0].BitRateValueMinus1 = 998
	if err := v.ValidateForSPS(s); err != nil {
		t.Fatalf("一个合规方案即满足Annex A: %v", err)
	}
	if err := v.ValidateResourceLimits(VUIResourceLimits{MaxBitRate: 64000, MaxCPBSize: 175000}); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("显式资源预算仍约束每个声明方案: %v", err)
	}
	// 另一个实际出现的HRD不能被已有的合规VCL掩盖。
	v.NALHRDParametersPresentFlag = true
	v.NALHRD = vuiLevelHRD(76864, 16)
	if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("NAL/VCL必须各自至少有一个合规方案: %v", err)
	}
}

func TestVUIAnnexALevel1bAndUnsupported(t *testing.T) {
	for _, profile := range []uint8{66, 77, 88} {
		s := vuiLevelSPS(profile, 11)
		s.ConstraintSetFlags[3] = true
		v := &VUI{VCLHRDParametersPresentFlag: true, VCLHRD: vuiLevelHRD(128000, 350000/16*16)}
		if err := v.ValidateForSPS(s); err != nil {
			t.Fatalf("profile%d Level1b: %v", profile, err)
		}
		v.VCLHRD.CPB[0].BitRateValueMinus1++
		if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("Level1b不可使用Level1.1较大上限: %v", err)
		}
		s.ConstraintSetFlags[3] = false
		if err := v.ValidateForSPS(s); err != nil {
			t.Fatalf("Level1.1较大上限: %v", err)
		}
		s.LevelIDC = 9
		if err := (&VUI{}).ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("旧profile不得用9表示Level1b: %v", err)
		}
		s.LevelIDC, s.ConstraintSetFlags[3] = 10, true
		if err := (&VUI{}).ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("旧profile的constraint_set3只适用于level_idc=11: %v", err)
		}
	}
	for _, profile := range []uint8{100, 110, 122, 244} {
		s := vuiLevelSPS(profile, 9)
		if err := (&VUI{}).ValidateForSPS(s); err != nil {
			t.Fatalf("High系列用level_idc=9表示Level1b: %v", err)
		}
		s.LevelIDC, s.ConstraintSetFlags[3], s.MaxNumRefFrames = 11, true, 1
		if err := (&VUI{}).ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("High系列constraint_set3是Intra声明，不是Level1b: %v", err)
		}
		s.MaxNumRefFrames = 0
		if err := (&VUI{}).ValidateForSPS(s); err != nil {
			t.Fatalf("合法Intra声明: %v", err)
		}
		if err := (&VUI{BitstreamRestrictionFlag: true, MaxDecFrameBuffering: 1}).ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("Intra声明不可含非零DPB信令: %v", err)
		}
	}
	for _, s := range []*SPS{vuiLevelSPS(44, 10), vuiLevelSPS(83, 10), vuiLevelSPS(100, 0), vuiLevelSPS(100, 63)} {
		if err := (&VUI{}).ValidateForSPS(s); !errors.Is(err, h264.ErrUnsupported) || errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("未知能力应区别于损坏语法: %+v / %v", s, err)
		}
	}
}

func TestVUIAnnexAPictureAndDPBBounds(t *testing.T) {
	s := vuiLevelSPS(100, 10)
	s.PicWidthInMbsMinus1, s.PicHeightInMapUnitsMinus1, s.MaxNumRefFrames = 10, 8, 4
	v := &VUI{BitstreamRestrictionFlag: true, MaxDecFrameBuffering: 4, MaxNumReorderFrames: 4}
	if err := v.ValidateForSPS(s); err != nil {
		t.Fatalf("11x9 MB、396/99=4帧DPB: %v", err)
	}
	v.MaxDecFrameBuffering = 5
	if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("DPB超过level上限: %v", err)
	}
	v.BitstreamRestrictionFlag = false
	s.MaxNumRefFrames = 5
	if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("无restriction仍须校验推导DPB与参考数: %v", err)
	}
	s.MaxNumRefFrames = 0
	s.PicWidthInMbsMinus1 = 11
	if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("108 MB超过MaxFS99: %v", err)
	}
	for _, dims := range [][2]uint32{{28, 0}, {0, 28}, {math.MaxUint32, math.MaxUint32}} {
		s.PicWidthInMbsMinus1, s.PicHeightInMapUnitsMinus1 = dims[0], dims[1]
		if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("超长单边/尺寸回绕: %v / %v", dims, err)
		}
	}
	// 隔行按完整帧高度而非map_unit高度计算；Level2.1允许隔行。
	s = vuiLevelSPS(100, 21)
	s.FrameMbsOnlyFlag = false
	s.PicWidthInMbsMinus1, s.PicHeightInMapUnitsMinus1 = 21, 17
	v.BitstreamRestrictionFlag, v.MaxDecFrameBuffering, v.MaxNumReorderFrames = true, 6, 6
	if err := v.ValidateForSPS(s); err != nil {
		t.Fatalf("22x36 MB、4752/792=6帧隔行DPB: %v", err)
	}
	v.MaxDecFrameBuffering = 7
	if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("隔行DPB未按完整帧计算: %v", err)
	}
}

func TestVUIAnnexAFrameAndDirectDeclarations(t *testing.T) {
	for _, level := range []uint8{9, 10, 11, 12, 13, 20, 42, 50, 51, 52, 60, 61, 62} {
		s := vuiLevelSPS(100, level)
		s.FrameMbsOnlyFlag = false
		if err := (&VUI{}).ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("level%d须逐行编码: %v", level, err)
		}
	}
	for _, profile := range []uint8{77, 88, 100, 110, 122, 244} {
		s := vuiLevelSPS(profile, 30)
		s.Direct8x8InferenceFlag = false
		if err := (&VUI{}).ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("profile%d level3须direct_8x8_inference: %v", profile, err)
		}
	}
	s := vuiLevelSPS(100, 30)
	s.Direct8x8InferenceFlag, s.ConstraintSetFlags[3] = false, true
	if err := (&VUI{}).ValidateForSPS(s); err != nil {
		t.Fatalf("Intra声明不使用B直接模式: %v", err)
	}
}

// 表 A-1 的MaxFS/MaxDpbMbs独立边界，覆盖Level1b直到6.2。
func TestVUIAnnexAAllLevelPictureBounds(t *testing.T) {
	for _, tc := range []struct {
		level              uint8
		width, height, dpb uint32
	}{
		{9, 11, 9, 4}, {10, 11, 9, 4}, {11, 22, 18, 2},
		{12, 22, 18, 6}, {13, 22, 18, 6}, {20, 22, 18, 6},
		{21, 22, 36, 6}, {22, 45, 36, 5}, {30, 45, 36, 5},
		{31, 80, 45, 5}, {32, 80, 64, 4}, {40, 128, 64, 4},
		{41, 128, 64, 4}, {42, 136, 64, 4}, {50, 160, 138, 5},
		{51, 256, 144, 5}, {52, 256, 144, 5},
		{60, 512, 272, 5}, {61, 512, 272, 5}, {62, 512, 272, 5},
	} {
		s := vuiLevelSPS(100, tc.level)
		s.PicWidthInMbsMinus1, s.PicHeightInMapUnitsMinus1 = tc.width-1, tc.height-1
		s.MaxNumRefFrames = tc.dpb
		v := &VUI{BitstreamRestrictionFlag: true, MaxDecFrameBuffering: tc.dpb}
		if err := v.ValidateForSPS(s); err != nil {
			t.Fatalf("level%d合法MaxFS/DPB边界: %v", tc.level, err)
		}
		v.MaxDecFrameBuffering++
		if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("level%d超MaxDpbFrames: %v", tc.level, err)
		}
		v.MaxDecFrameBuffering--
		s.PicHeightInMapUnitsMinus1++
		if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("level%d超MaxFS: %v", tc.level, err)
		}
	}
	// 较小画面计算值再大也必须取min(...,16)，不因高级别放宽到17。
	s := vuiLevelSPS(100, 62)
	s.MaxNumRefFrames = 16
	if err := (&VUI{}).ValidateForSPS(s); err != nil {
		t.Fatal(err)
	}
	s.MaxNumRefFrames = 17
	if err := (&VUI{}).ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("DPB全局16帧上限: %v", err)
	}
}
