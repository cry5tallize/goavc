package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// Validate 校验不依赖 SPS 上下文的 E.2.1/E.2.2 字段语义。
// 仅校验按 present_flag 实际出现的字段，不把缺省字段的 Go 零值当作
// 规范推导值。保留的 SAR、video_format、colour 枚举不作损坏处理，
// 也不改写为其它枚举；本层不解释这些保留值的显示含义。
// profile/level 约束由 ValidateForSPS 补充；实际码流是否满足声明的
// 重排、运动矢量和时序约束，需要解码上下文另行校验。
func (v *VUI) Validate() error {
	if v == nil {
		return fmt.Errorf("syntax: nil vui: %w", h264.ErrInvalidData)
	}
	if v.AspectRatioInfoPresentFlag && v.AspectRatioIDC == aspectRatioExtendedSAR &&
		v.SARWidth != 0 && v.SARHeight != 0 {
		// E.2.1：零表示未指定；非零宽高必须互质，不能自行约分改变信令。
		a, b := v.SARWidth, v.SARHeight
		for b != 0 {
			a, b = b, a%b
		}
		if a != 1 {
			return fmt.Errorf("syntax: sar_width/sar_height = %d/%d are not relatively prime: %w",
				v.SARWidth, v.SARHeight, h264.ErrInvalidData)
		}
	}
	if v.VideoSignalTypePresentFlag && v.VideoFormat > 7 {
		return fmt.Errorf("syntax: video_format = %d exceeds 3 bits: %w", v.VideoFormat, h264.ErrInvalidData)
	}
	if v.ChromaLocInfoPresentFlag &&
		(v.ChromaSampleLocTypeTopField > 5 || v.ChromaSampleLocTypeBottomField > 5) {
		return fmt.Errorf("syntax: chroma_sample_loc_type = %d/%d exceeds 5: %w",
			v.ChromaSampleLocTypeTopField, v.ChromaSampleLocTypeBottomField, h264.ErrInvalidData)
	}
	if v.TimingInfoPresentFlag && (v.NumUnitsInTick == 0 || v.TimeScale == 0) {
		return fmt.Errorf("syntax: timing num_units_in_tick/time_scale must be positive: %w", h264.ErrInvalidData)
	}
	if v.NALHRDParametersPresentFlag {
		if err := v.NALHRD.Validate(); err != nil {
			return fmt.Errorf("syntax: nal hrd_parameters: %w", err)
		}
	}
	if v.VCLHRDParametersPresentFlag {
		if err := v.VCLHRD.Validate(); err != nil {
			return fmt.Errorf("syntax: vcl hrd_parameters: %w", err)
		}
	}
	if v.NALHRDParametersPresentFlag && v.VCLHRDParametersPresentFlag {
		nal, vcl := v.NALHRD, v.VCLHRD
		if nal.InitialCPBRemovalDelayLengthMinus1 != vcl.InitialCPBRemovalDelayLengthMinus1 ||
			nal.CPBRemovalDelayLengthMinus1 != vcl.CPBRemovalDelayLengthMinus1 ||
			nal.DPBOutputDelayLengthMinus1 != vcl.DPBOutputDelayLengthMinus1 ||
			nal.TimeOffsetLength != vcl.TimeOffsetLength {
			return fmt.Errorf("syntax: NAL/VCL HRD delay lengths must match: %w", h264.ErrInvalidData)
		}
	}
	if (v.NALHRDParametersPresentFlag || v.VCLHRDParametersPresentFlag) && v.LowDelayHRDFlag {
		if v.TimingInfoPresentFlag && v.FixedFrameRateFlag {
			return fmt.Errorf("syntax: fixed_frame_rate_flag requires low_delay_hrd_flag = 0: %w", h264.ErrInvalidData)
		}
		if (v.NALHRDParametersPresentFlag && v.NALHRD.CPBCountMinus1 != 0) ||
			(v.VCLHRDParametersPresentFlag && v.VCLHRD.CPBCountMinus1 != 0) {
			return fmt.Errorf("syntax: low_delay_hrd_flag requires cpb_cnt_minus1 = 0: %w", h264.ErrInvalidData)
		}
	}
	if v.BitstreamRestrictionFlag {
		if v.MaxBytesPerPicDenom > 16 || v.MaxBitsPerMBDenom > 16 {
			return fmt.Errorf("syntax: bitstream restriction denominators exceed 16: %w", h264.ErrInvalidData)
		}
		if v.Log2MaxMVLengthHorizontal > 15 || v.Log2MaxMVLengthVertical > 15 {
			return fmt.Errorf("syntax: log2_max_mv_length exceeds 15: %w", h264.ErrInvalidData)
		}
		// Annex A 的 MaxDpbFrames 无论 level 均不超过 16；更小的 level
		// 上限及 max_num_ref_frames 下限必须结合 SPS 判断。
		if v.MaxDecFrameBuffering > 16 || v.MaxNumReorderFrames > v.MaxDecFrameBuffering {
			return fmt.Errorf("syntax: invalid reorder/DPB bounds %d/%d: %w",
				v.MaxNumReorderFrames, v.MaxDecFrameBuffering, h264.ErrInvalidData)
		}
	}
	return nil
}

// ValidateForSPS 补充依赖 SPS 的 Annex A/E 语义；不改写未出现字段的推导值。
// 未携带 VUI 的 SPS 可用零值 VUI 调用，仍校验 level 与推导的 DPB 约束。
func (v *VUI) ValidateForSPS(s *SPS) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("syntax: VUI validation requires an SPS: %w", h264.ErrInvalidData)
	}
	if v.BitstreamRestrictionFlag && v.MaxDecFrameBuffering < s.MaxNumRefFrames {
		return fmt.Errorf("syntax: max_dec_frame_buffering %d is less than max_num_ref_frames %d: %w",
			v.MaxDecFrameBuffering, s.MaxNumRefFrames, h264.ErrInvalidData)
	}
	return v.validateLevelForSPS(s)
}

// VUIResourceLimits 是接收者明确配置的声明预算，0 表示该项未设置限制。
// MaxBitRate 单位为 bit/s，MaxCPBSize 单位为 bit；它们不是 RSS 字节预算。
// 每个 CPB 是替代调度方案，NAL/VCL HRD 也是不同的记账方式，不求和。
type VUIResourceLimits struct {
	MaxDPBFrames uint32
	MaxBitRate   uint64
	MaxCPBSize   uint64
}

// ValidateResourceLimits 先检查语法语义，再检查每个实际出现的资源声明。
// 损坏语法返回 ErrInvalidData，超过非零配置预算返回 ErrResourceLimit。
// 未出现的 VUI DPB 字段不在此推导；接收者还须按 SPS 检查实际 DPB 需求。
func (v *VUI) ValidateResourceLimits(limits VUIResourceLimits) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.BitstreamRestrictionFlag && limits.MaxDPBFrames != 0 && v.MaxDecFrameBuffering > limits.MaxDPBFrames {
		return fmt.Errorf("syntax: VUI DPB %d exceeds limit %d: %w", v.MaxDecFrameBuffering, limits.MaxDPBFrames, h264.ErrResourceLimit)
	}
	for _, item := range [...]struct {
		present bool
		name    string
		hrd     *HRDParameters
	}{
		{v.NALHRDParametersPresentFlag, "NAL", v.NALHRD},
		{v.VCLHRDParametersPresentFlag, "VCL", v.VCLHRD},
	} {
		if !item.present {
			continue
		}
		for i, cpb := range item.hrd.CPB {
			if limits.MaxBitRate != 0 {
				rate := hrdValue(cpb.BitRateValueMinus1, 6+item.hrd.BitRateScale)
				if rate > limits.MaxBitRate {
					return fmt.Errorf("syntax: %s HRD CPB %d bit rate %d exceeds limit %d: %w", item.name, i, rate, limits.MaxBitRate, h264.ErrResourceLimit)
				}
			}
			if limits.MaxCPBSize != 0 {
				size := hrdValue(cpb.CPBSizeValueMinus1, 4+item.hrd.CPBSizeScale)
				if size > limits.MaxCPBSize {
					return fmt.Errorf("syntax: %s HRD CPB %d size %d exceeds limit %d: %w", item.name, i, size, limits.MaxCPBSize, h264.ErrResourceLimit)
				}
			}
		}
	}
	return nil
}
