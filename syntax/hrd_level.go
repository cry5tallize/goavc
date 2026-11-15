package syntax

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// hrdLevelLimits 对应 H.264 (08/2024) 表 A-1；码率和容量尚未乘 profile 倍率。
// levelIDC=9 在 High 系列中表示 Level 1b；旧 profile 的 11+constraint_set3
// 先归一为 9。所有字段保持整数，避免尺寸乘法和倍率计算发生回绕。
type hrdLevelLimits struct {
	levelIDC  uint8
	maxFS     uint64
	maxDpbMbs uint64
	maxBR     uint64
	maxCPB    uint64
}

var hrdLevels = [...]hrdLevelLimits{
	{10, 99, 396, 64, 175},
	{9, 99, 396, 128, 350},
	{11, 396, 900, 192, 500},
	{12, 396, 2376, 384, 1000},
	{13, 396, 2376, 768, 2000},
	{20, 396, 2376, 2000, 2000},
	{21, 792, 4752, 4000, 4000},
	{22, 1620, 8100, 4000, 4000},
	{30, 1620, 8100, 10000, 10000},
	{31, 3600, 18000, 14000, 14000},
	{32, 5120, 20480, 20000, 20000},
	{40, 8192, 32768, 20000, 25000},
	{41, 8192, 32768, 50000, 62500},
	{42, 8704, 34816, 50000, 62500},
	{50, 22080, 110400, 135000, 135000},
	{51, 36864, 184320, 240000, 240000},
	{52, 36864, 184320, 240000, 240000},
	{60, 139264, 696320, 240000, 240000},
	{61, 139264, 696320, 480000, 480000},
	{62, 139264, 696320, 800000, 800000},
}

// validateLevelForSPS 只接收已通过 VUI.Validate 的结构。这里只检查参数集
// 能决定的 Annex A 约束，不把实际 AU 码率、SEI 时序或运动矢量限制伪装为已验证。
func (v *VUI) validateLevelForSPS(s *SPS) error {
	var vclFactor, nalFactor uint64
	legacy := false
	switch s.ProfileIDC {
	case 66, 77, 88:
		vclFactor, nalFactor, legacy = 1000, 1200, true
	case 100:
		vclFactor, nalFactor = 1250, 1500
	case 110:
		vclFactor, nalFactor = 3000, 3600
	case 122, 244:
		vclFactor, nalFactor = 4000, 4800
	default:
		return fmt.Errorf("syntax: HRD level validation for profile_idc %d: %w", s.ProfileIDC, h264.ErrUnsupported)
	}
	levelIDC := s.LevelIDC
	if legacy {
		if levelIDC == 9 {
			return fmt.Errorf("syntax: profile %d signals Level 1b using level_idc=11 and constraint_set3_flag: %w", s.ProfileIDC, h264.ErrInvalidData)
		}
		if s.ConstraintSetFlags[3] {
			if levelIDC != 11 {
				return fmt.Errorf("syntax: constraint_set3_flag is reserved for profile %d at level_idc %d: %w", s.ProfileIDC, levelIDC, h264.ErrInvalidData)
			}
			levelIDC = 9
		}
	}
	var level *hrdLevelLimits
	for i := range hrdLevels {
		if hrdLevels[i].levelIDC == levelIDC {
			level = &hrdLevels[i]
			break
		}
	}
	if level == nil {
		return fmt.Errorf("syntax: level_idc %d is not supported: %w", s.LevelIDC, h264.ErrUnsupported)
	}
	width := uint64(s.PicWidthInMbsMinus1) + 1
	height := uint64(s.PicHeightInMapUnitsMinus1) + 1
	if !s.FrameMbsOnlyFlag {
		height *= 2
	}
	// 先比较单边，再乘尺寸，连 uint32 最大字段也不会令 uint64 乘积溢出。
	if width > level.maxFS || height > level.maxFS || width*height > level.maxFS ||
		width*width > 8*level.maxFS || height*height > 8*level.maxFS {
		return fmt.Errorf("syntax: picture %dx%d macroblocks exceeds level_idc %d frame bounds: %w", width, height, s.LevelIDC, h264.ErrInvalidData)
	}
	// 表 A-4/A-5 的逐行编码约束，以及 profile/constraint_set4 的逐行声明。
	progressive := s.ProfileIDC == 66 || levelIDC <= 20 || levelIDC >= 42 ||
		((s.ProfileIDC == 77 || s.ProfileIDC == 88 || s.ProfileIDC == 100 || s.ProfileIDC == 110) && s.ConstraintSetFlags[4])
	if progressive && !s.FrameMbsOnlyFlag {
		return fmt.Errorf("syntax: profile %d level_idc %d requires frame_mbs_only_flag: %w", s.ProfileIDC, s.LevelIDC, h264.ErrInvalidData)
	}
	intra := !legacy && s.ConstraintSetFlags[3]
	noB := s.ProfileIDC == 66 || intra ||
		((s.ProfileIDC == 77 || s.ProfileIDC == 88 || s.ProfileIDC == 100) && s.ConstraintSetFlags[5])
	if (s.ProfileIDC == 88 || (levelIDC >= 30 && !noB)) && !s.Direct8x8InferenceFlag {
		return fmt.Errorf("syntax: profile %d level_idc %d requires direct_8x8_inference_flag: %w", s.ProfileIDC, s.LevelIDC, h264.ErrInvalidData)
	}
	maxDPB := min(level.maxDpbMbs/(width*height), uint64(16))
	if uint64(s.MaxNumRefFrames) > maxDPB {
		return fmt.Errorf("syntax: max_num_ref_frames %d exceeds level DPB %d: %w", s.MaxNumRefFrames, maxDPB, h264.ErrInvalidData)
	}
	if intra && (s.MaxNumRefFrames != 0 ||
		(v.BitstreamRestrictionFlag && (v.MaxDecFrameBuffering != 0 || v.MaxNumReorderFrames != 0))) {
		return fmt.Errorf("syntax: intra profile requires zero reference/reorder/DPB declarations: %w", h264.ErrInvalidData)
	}
	if v.BitstreamRestrictionFlag && uint64(v.MaxDecFrameBuffering) > maxDPB {
		return fmt.Errorf("syntax: max_dec_frame_buffering %d exceeds level DPB %d: %w", v.MaxDecFrameBuffering, maxDPB, h264.ErrInvalidData)
	}
	if v.NALHRDParametersPresentFlag {
		if err := validateHRDLevel(v.NALHRD, "NAL", nalFactor*level.maxBR, nalFactor*level.maxCPB); err != nil {
			return err
		}
	}
	if v.VCLHRDParametersPresentFlag {
		if err := validateHRDLevel(v.VCLHRD, "VCL", vclFactor*level.maxBR, vclFactor*level.maxCPB); err != nil {
			return err
		}
	}
	return nil
}

// validateHRDLevel 按 A.3.1 i/j、A.3.3 g/h，至少一个替代调度方案必须同时
// 满足码率与容量上限。不能分别找两个不同方案，也不能要求所有方案均满足。
func validateHRDLevel(h *HRDParameters, name string, maxRate, maxSize uint64) error {
	for _, cpb := range h.CPB {
		if hrdValue(cpb.BitRateValueMinus1, 6+h.BitRateScale) <= maxRate &&
			hrdValue(cpb.CPBSizeValueMinus1, 4+h.CPBSizeScale) <= maxSize {
			return nil
		}
	}
	return fmt.Errorf("syntax: %s HRD has no CPB schedule within level bit rate/size %d/%d: %w", name, maxRate, maxSize, h264.ErrInvalidData)
}
