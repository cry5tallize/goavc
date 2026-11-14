package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// SPS 是 sequence parameter set 的解析结果（H.264 §7.3.2.1）。
//
// 字段按规范顺序排列。非 High 系列 profile 不携带色度/位深字段，
// 解析时会补上默认值 ChromaFormatIDC=1、BitDepth*Minus8=0，
// 这样消费方可以统一使用这些字段。
//
// 未实现的工具组合（422/444、隔行、扩展profile）
// 在解析结束时返回 ErrUnsupported，而不是把不完整的结果交给上层。
type SPS struct {
	ProfileIDC         uint8
	ConstraintSetFlags [6]bool
	ReservedZero2Bits  uint8
	LevelIDC           uint8
	SeqParameterSetID  uint32

	ChromaFormatIDC                  uint32
	SeparateColourPlaneFlag          bool
	BitDepthLumaMinus8               uint32
	BitDepthChromaMinus8             uint32
	QPPPrimeYZeroTransformBypassFlag bool
	SeqScalingMatrixPresentFlag      bool
	SeqScalingListPresentFlag        [12]bool
	SeqScalingList                   [12]*ScalingList

	Log2MaxFrameNumMinus4          uint32
	PicOrderCntType                uint32
	Log2MaxPicOrderCntLsbMinus4    uint32
	DeltaPicOrderAlwaysZeroFlag    bool
	OffsetForNonRefPic             int32
	OffsetForTopToBottomField      int32
	NumRefFramesInPicOrderCntCycle uint32
	OffsetForRefFrame              []int32

	MaxNumRefFrames                uint32
	GapsInFrameNumValueAllowedFlag bool
	PicWidthInMbsMinus1            uint32
	PicHeightInMapUnitsMinus1      uint32
	FrameMbsOnlyFlag               bool
	MbAdaptiveFrameFieldFlag       bool
	Direct8x8InferenceFlag         bool
	FrameCroppingFlag              bool
	FrameCropLeftOffset            uint32
	FrameCropRightOffset           uint32
	FrameCropTopOffset             uint32
	FrameCropBottomOffset          uint32

	VUIParametersPresentFlag bool
	VUI                      *VUI
}

// ParseSPS 解析一个完整的 SPS RBSP（由 EBSP 去除防竞争字节后得到）。
//
// 输入必须包含 rbsp_trailing_bits；解析结束时会校验其后的位模式，
// 多余字节或缺少停止位都返回 ErrInvalidData。
func ParseSPS(rbsp []byte) (*SPS, error) {
	r := bitstream.NewReader(rbsp)
	s := &SPS{ChromaFormatIDC: 1}

	profile, err := readBits(r, 8, "profile_idc")
	if err != nil {
		return nil, err
	}
	s.ProfileIDC = uint8(profile)

	constraint, err := readBits(r, 8, "constraint_set flags")
	if err != nil {
		return nil, err
	}
	for i := 0; i < 6; i++ {
		s.ConstraintSetFlags[i] = constraint&(1<<(7-i)) != 0
	}
	s.ReservedZero2Bits = uint8(constraint & 0x03)
	if s.ReservedZero2Bits != 0 {
		return nil, fmt.Errorf("syntax: sps reserved_zero_2bits = %d, must be 0: %w",
			s.ReservedZero2Bits, h264.ErrInvalidData)
	}

	level, err := readBits(r, 8, "level_idc")
	if err != nil {
		return nil, err
	}
	s.LevelIDC = uint8(level)

	if s.SeqParameterSetID, err = readUE(r, "seq_parameter_set_id"); err != nil {
		return nil, err
	}
	if s.SeqParameterSetID > 31 {
		return nil, fmt.Errorf("syntax: seq_parameter_set_id = %d exceeds 31: %w",
			s.SeqParameterSetID, h264.ErrInvalidData)
	}

	if usesHighProfileSyntax(s.ProfileIDC) {
		if err := s.parseHighProfileFields(r); err != nil {
			return nil, err
		}
	}

	if s.Log2MaxFrameNumMinus4, err = readUE(r, "log2_max_frame_num_minus4"); err != nil {
		return nil, err
	}
	if s.Log2MaxFrameNumMinus4 > 12 {
		return nil, fmt.Errorf("syntax: log2_max_frame_num_minus4 = %d exceeds 12: %w",
			s.Log2MaxFrameNumMinus4, h264.ErrInvalidData)
	}

	if s.PicOrderCntType, err = readUE(r, "pic_order_cnt_type"); err != nil {
		return nil, err
	}
	switch s.PicOrderCntType {
	case 0:
		if s.Log2MaxPicOrderCntLsbMinus4, err = readUE(r, "log2_max_pic_order_cnt_lsb_minus4"); err != nil {
			return nil, err
		}
		if s.Log2MaxPicOrderCntLsbMinus4 > 12 {
			return nil, fmt.Errorf("syntax: log2_max_pic_order_cnt_lsb_minus4 = %d exceeds 12: %w",
				s.Log2MaxPicOrderCntLsbMinus4, h264.ErrInvalidData)
		}
	case 1:
		if s.DeltaPicOrderAlwaysZeroFlag, err = readFlag(r, "delta_pic_order_always_zero_flag"); err != nil {
			return nil, err
		}
		if s.OffsetForNonRefPic, err = readSE(r, "offset_for_non_ref_pic"); err != nil {
			return nil, err
		}
		if s.OffsetForTopToBottomField, err = readSE(r, "offset_for_top_to_bottom_field"); err != nil {
			return nil, err
		}
		if s.NumRefFramesInPicOrderCntCycle, err = readUE(r, "num_ref_frames_in_pic_order_cnt_cycle"); err != nil {
			return nil, err
		}
		if s.NumRefFramesInPicOrderCntCycle > 255 {
			return nil, fmt.Errorf("syntax: num_ref_frames_in_pic_order_cnt_cycle = %d exceeds 255: %w",
				s.NumRefFramesInPicOrderCntCycle, h264.ErrInvalidData)
		}
		s.OffsetForRefFrame = make([]int32, s.NumRefFramesInPicOrderCntCycle)
		for i := range s.OffsetForRefFrame {
			if s.OffsetForRefFrame[i], err = readSE(r, "offset_for_ref_frame"); err != nil {
				return nil, err
			}
		}
	case 2:
		// 无额外字段。
	default:
		return nil, fmt.Errorf("syntax: pic_order_cnt_type = %d exceeds 2: %w",
			s.PicOrderCntType, h264.ErrInvalidData)
	}

	if s.MaxNumRefFrames, err = readUE(r, "max_num_ref_frames"); err != nil {
		return nil, err
	}
	if s.GapsInFrameNumValueAllowedFlag, err = readFlag(r, "gaps_in_frame_num_value_allowed_flag"); err != nil {
		return nil, err
	}
	if s.PicWidthInMbsMinus1, err = readUE(r, "pic_width_in_mbs_minus1"); err != nil {
		return nil, err
	}
	if s.PicHeightInMapUnitsMinus1, err = readUE(r, "pic_height_in_map_units_minus1"); err != nil {
		return nil, err
	}
	if s.FrameMbsOnlyFlag, err = readFlag(r, "frame_mbs_only_flag"); err != nil {
		return nil, err
	}
	if !s.FrameMbsOnlyFlag {
		if s.MbAdaptiveFrameFieldFlag, err = readFlag(r, "mb_adaptive_frame_field_flag"); err != nil {
			return nil, err
		}
	}
	if s.Direct8x8InferenceFlag, err = readFlag(r, "direct_8x8_inference_flag"); err != nil {
		return nil, err
	}

	if s.FrameCroppingFlag, err = readFlag(r, "frame_cropping_flag"); err != nil {
		return nil, err
	}
	if s.FrameCroppingFlag {
		if s.FrameCropLeftOffset, err = readUE(r, "frame_crop_left_offset"); err != nil {
			return nil, err
		}
		if s.FrameCropRightOffset, err = readUE(r, "frame_crop_right_offset"); err != nil {
			return nil, err
		}
		if s.FrameCropTopOffset, err = readUE(r, "frame_crop_top_offset"); err != nil {
			return nil, err
		}
		if s.FrameCropBottomOffset, err = readUE(r, "frame_crop_bottom_offset"); err != nil {
			return nil, err
		}
	}

	if s.VUIParametersPresentFlag, err = readFlag(r, "vui_parameters_present_flag"); err != nil {
		return nil, err
	}
	if s.VUIParametersPresentFlag {
		if s.VUI, err = ParseVUI(r); err != nil {
			return nil, fmt.Errorf("syntax: sps vui_parameters: %w", err)
		}
	}

	if err := consumeRBSPTrailingBits(r); err != nil {
		return nil, fmt.Errorf("syntax: sps trailing bits: %w", err)
	}
	if err := s.validateSupported(); err != nil {
		return nil, err
	}
	return s, nil
}

// parseHighProfileFields 解析 profile_idc 100/110/122/244 等携带的扩展字段。
func (s *SPS) parseHighProfileFields(r *bitstream.Reader) error {
	var err error
	if s.ChromaFormatIDC, err = readUE(r, "chroma_format_idc"); err != nil {
		return err
	}
	if s.ChromaFormatIDC > 3 {
		return fmt.Errorf("syntax: chroma_format_idc = %d exceeds 3: %w",
			s.ChromaFormatIDC, h264.ErrInvalidData)
	}
	if s.ChromaFormatIDC == 3 {
		if s.SeparateColourPlaneFlag, err = readFlag(r, "separate_colour_plane_flag"); err != nil {
			return err
		}
	}
	if s.BitDepthLumaMinus8, err = readUE(r, "bit_depth_luma_minus8"); err != nil {
		return err
	}
	if s.BitDepthChromaMinus8, err = readUE(r, "bit_depth_chroma_minus8"); err != nil {
		return err
	}
	if s.BitDepthLumaMinus8 > 6 || s.BitDepthChromaMinus8 > 6 {
		return fmt.Errorf("syntax: bit depth minus8 (%d/%d) exceeds 6: %w",
			s.BitDepthLumaMinus8, s.BitDepthChromaMinus8, h264.ErrInvalidData)
	}
	if s.QPPPrimeYZeroTransformBypassFlag, err = readFlag(r, "qpprime_y_zero_transform_bypass_flag"); err != nil {
		return err
	}
	if s.SeqScalingMatrixPresentFlag, err = readFlag(r, "seq_scaling_matrix_present_flag"); err != nil {
		return err
	}
	if !s.SeqScalingMatrixPresentFlag {
		return nil
	}

	count := 8
	if s.ChromaFormatIDC == 3 {
		count = 12
	}
	for i := 0; i < count; i++ {
		present, err := readFlag(r, "seq_scaling_list_present_flag")
		if err != nil {
			return err
		}
		s.SeqScalingListPresentFlag[i] = present
		if !present {
			continue
		}
		size := 4
		if i >= 6 {
			size = 8
		}
		list, err := ParseScalingList(r, size)
		if err != nil {
			return fmt.Errorf("syntax: sps seq_scaling_list[%d]: %w", i, err)
		}
		s.SeqScalingList[i] = list
	}
	return nil
}

// usesHighProfileSyntax 判断该 profile_idc 是否携带 High 系列扩展字段。
//
// 列表来自 §7.3.2.1；先读取完整字段，再由validateSupported限制已实现组合。
func usesHighProfileSyntax(profile uint8) bool {
	switch profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		return true
	default:
		return false
	}
}

// validateSupported 限制已经接入共享内核与流水线的工具组合。
func (s *SPS) validateSupported() error {
	switch s.ProfileIDC {
	case 66, 77, 88, 100, 110, 122, 244:
	default:
		return fmt.Errorf("syntax: profile_idc %d is not supported: %w",
			s.ProfileIDC, h264.ErrUnsupported)
	}
	if usesHighProfileSyntax(s.ProfileIDC) {
		if s.SeparateColourPlaneFlag && s.ChromaFormatIDC != 3 {
			return fmt.Errorf("syntax: separate colour planes require chroma_format_idc=3: %w", h264.ErrInvalidData)
		}
		if s.QPPPrimeYZeroTransformBypassFlag {
			return fmt.Errorf("syntax: qpprime_y_zero_transform_bypass_flag is not supported: %w", h264.ErrUnsupported)
		}
		maxChroma := uint32(1)
		if s.ProfileIDC == 122 {
			maxChroma = 2
		}
		if s.ProfileIDC == 244 {
			maxChroma = 3
		}
		if s.ChromaFormatIDC < 1 || s.ChromaFormatIDC > maxChroma {
			return fmt.Errorf("syntax: chroma_format_idc %d is not supported for profile %d: %w",
				s.ChromaFormatIDC, s.ProfileIDC, h264.ErrUnsupported)
		}
		high10 := (s.ProfileIDC == 110 || s.ProfileIDC == 122 || s.ProfileIDC == 244) && s.BitDepthLumaMinus8 == 2 && s.BitDepthChromaMinus8 == 2
		if !high10 && (s.BitDepthLumaMinus8 != 0 || s.BitDepthChromaMinus8 != 0) {
			return fmt.Errorf("syntax: unsupported bit depth %d/%d for profile %d: %w",
				s.BitDepthLumaMinus8+8, s.BitDepthChromaMinus8+8, s.ProfileIDC, h264.ErrUnsupported)
		}
	}
	// Spec: 7.4.2.1.1与Annex A；字段语法可解析不代表流水线已支持。
	if s.FrameMbsOnlyFlag && s.MbAdaptiveFrameFieldFlag {
		return fmt.Errorf("syntax: MBAFF requires frame_mbs_only_flag=0: %w", h264.ErrInvalidData)
	}
	if !s.FrameMbsOnlyFlag && (!s.Direct8x8InferenceFlag || s.ProfileIDC == 66) {
		return fmt.Errorf("syntax: field coding requires direct_8x8_inference and non-Baseline profile: %w", h264.ErrInvalidData)
	}
	return nil
}

// effectiveChromaFormatIDC 返回实际生效的色度格式。
//
// 非 High 系列 profile 的语法里没有 chroma_format_idc，按规范隐含为 1
// （4:2:0）。这样手工构造的 SPS 即使没有显式填 ChromaFormatIDC 也能正确
// 计算平面尺寸。
func (s *SPS) effectiveChromaFormatIDC() uint32 {
	if !usesHighProfileSyntax(s.ProfileIDC) {
		return 1
	}
	return s.ChromaFormatIDC
}

// SubWidthC 返回亮度到色度的水平抽样因子（§6.2）。
func (s *SPS) SubWidthC() uint64 {
	switch s.effectiveChromaFormatIDC() {
	case 1, 2:
		return 2
	default:
		return 1
	}
}

// SubHeightC 返回亮度到色度的垂直抽样因子（§6.2）。
func (s *SPS) SubHeightC() uint64 {
	switch s.effectiveChromaFormatIDC() {
	case 1:
		return 2
	case 2:
		return 1
	default:
		return 1
	}
}

// PicWidthInMbs 返回以宏块为单位的图像宽度。
func (s *SPS) PicWidthInMbs() uint64 { return uint64(s.PicWidthInMbsMinus1) + 1 }

// PicHeightInMapUnits 返回以宏块对/宏块为单位的图像高度（未乘帧场因子）。
func (s *SPS) PicHeightInMapUnits() uint64 { return uint64(s.PicHeightInMapUnitsMinus1) + 1 }

// FrameHeightInMbs 返回完整帧的宏块行数；隔行序列的map unit高度须乘2。
func (s *SPS) FrameHeightInMbs() uint64 {
	if s.FrameMbsOnlyFlag {
		return s.PicHeightInMapUnits()
	}
	return s.PicHeightInMapUnits() * 2
}

// DisplayWidth 返回按裁剪窗口计算后的显示宽度（像素）。
func (s *SPS) DisplayWidth() (uint64, error) {
	width := s.PicWidthInMbs() * 16
	crop := (uint64(s.FrameCropLeftOffset) + uint64(s.FrameCropRightOffset)) * s.SubWidthC()
	if crop > width {
		return 0, fmt.Errorf("syntax: crop width %d exceeds coded width %d: %w",
			crop, width, h264.ErrInvalidData)
	}
	return width - crop, nil
}

// DisplayHeight 返回按裁剪窗口计算后的显示高度（像素）。
func (s *SPS) DisplayHeight() (uint64, error) {
	height := s.PicHeightInMapUnits() * 16 * (2 - boolToUint64(s.FrameMbsOnlyFlag))
	cropUnitY := s.SubHeightC() * (2 - boolToUint64(s.FrameMbsOnlyFlag))
	crop := (uint64(s.FrameCropTopOffset) + uint64(s.FrameCropBottomOffset)) * cropUnitY
	if crop > height {
		return 0, fmt.Errorf("syntax: crop height %d exceeds coded height %d: %w",
			crop, height, h264.ErrInvalidData)
	}
	return height - crop, nil
}

func boolToUint64(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}
