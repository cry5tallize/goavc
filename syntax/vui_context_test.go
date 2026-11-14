package syntax

import (
	"errors"
	"math"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestVUIContextSemanticCombination(t *testing.T) {
	s := &SPS{ProfileIDC: 100, LevelIDC: 10, ChromaFormatIDC: 1, MaxNumRefFrames: 2, FrameMbsOnlyFlag: true}
	v := &VUI{BitstreamRestrictionFlag: true, MaxDecFrameBuffering: 2, MaxNumReorderFrames: 1}
	if err := v.ValidateForSPS(s); err != nil {
		t.Fatal(err)
	}
	v.MaxDecFrameBuffering = 1
	if err := v.ValidateForSPS(s); !errors.Is(err, h264.ErrInvalidData) || errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("VUI缓冲小于参考帧数必须是语义错误: %v", err)
	}
	v.BitstreamRestrictionFlag = false
	if err := v.ValidateForSPS(s); err != nil {
		t.Fatalf("未出现的DPB字段不可约束SPS: %v", err)
	}
	v.ChromaLocInfoPresentFlag = true
	for _, chroma := range []uint32{0, 2, 3} {
		s.ChromaFormatIDC = chroma
		if err := v.ValidateForSPS(s); err != nil {
			t.Fatalf("色度格式%d应忽略chroma_loc_info而非拒绝: %v", chroma, err)
		}
	}
	s.ChromaFormatIDC = 1
	if err := v.ValidateForSPS(s); err != nil {
		t.Fatal(err)
	}
	if err := v.ValidateForSPS(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("nil SPS: %v", err)
	}
}

// 每个CPB是可选的调度方案，不能把替代方案或NAL/VCL两种记账相加。
func TestVUIResourceLimitsAllSchedules(t *testing.T) {
	v := &VUI{
		NALHRDParametersPresentFlag: true,
		NALHRD:                      &HRDParameters{CPBCountMinus1: 1, CPB: []CPBEntry{{BitRateValueMinus1: 0, CPBSizeValueMinus1: 3}, {BitRateValueMinus1: 1, CPBSizeValueMinus1: 1}}},
		VCLHRDParametersPresentFlag: true,
		VCLHRD:                      &HRDParameters{CPB: []CPBEntry{{BitRateValueMinus1: 1, CPBSizeValueMinus1: 3}}},
		BitstreamRestrictionFlag:    true, MaxDecFrameBuffering: 4, MaxNumReorderFrames: 2,
	}
	limits := VUIResourceLimits{MaxDPBFrames: 4, MaxBitRate: 128, MaxCPBSize: 64}
	if err := v.ValidateResourceLimits(limits); err != nil {
		t.Fatalf("恰好满足每个调度方案: %v", err)
	}
	for _, limit := range []VUIResourceLimits{
		{MaxDPBFrames: 3}, {MaxBitRate: 127}, {MaxCPBSize: 63},
	} {
		if err := v.ValidateResourceLimits(limit); !errors.Is(err, h264.ErrResourceLimit) || errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("配置预算必须是资源错误: %+v / %v", limit, err)
		}
	}
	v.NALHRDParametersPresentFlag = false
	if err := v.ValidateResourceLimits(VUIResourceLimits{MaxBitRate: 127}); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("遗漏VCL HRD: %v", err)
	}
	v.VCLHRDParametersPresentFlag = false
	v.BitstreamRestrictionFlag = false
	if err := v.ValidateResourceLimits(VUIResourceLimits{MaxDPBFrames: 1, MaxBitRate: 1, MaxCPBSize: 1}); err != nil {
		t.Fatalf("不存在字段不得占用预算: %v", err)
	}
	v.BitstreamRestrictionFlag = true
	v.MaxDecFrameBuffering = 17
	if err := v.ValidateResourceLimits(limits); !errors.Is(err, h264.ErrInvalidData) || errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("损坏语法优先于配置预算: %v", err)
	}
}

func TestVUIResourceLimitsWideHRDValues(t *testing.T) {
	v := &VUI{NALHRDParametersPresentFlag: true, NALHRD: &HRDParameters{
		BitRateScale: 15, CPBSizeScale: 15,
		CPB: []CPBEntry{{BitRateValueMinus1: math.MaxUint32 - 1, CPBSizeValueMinus1: math.MaxUint32 - 1}},
	}}
	rate := uint64(math.MaxUint32) << 21
	size := uint64(math.MaxUint32) << 19
	if err := v.ValidateResourceLimits(VUIResourceLimits{MaxBitRate: rate, MaxCPBSize: size}); err != nil {
		t.Fatalf("64位精确预算: %v", err)
	}
	if err := v.ValidateResourceLimits(VUIResourceLimits{MaxBitRate: rate - 1}); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("码率不能回绕: %v", err)
	}
	if err := v.ValidateResourceLimits(VUIResourceLimits{MaxCPBSize: size - 1}); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("CPB容量不能回绕: %v", err)
	}
}
