package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// 规范依据：H.264，7.4.2.1.1、7.4.2.2、Annex A.2.1–A.2.10。
// https://www.itu.int/rec/dologin_pub.asp?id=T-REC-H.264-201304-S!!PDF-E&lang=e&type=items
// 这些函数只判断已知 profile 的合法性，不把合法的单色、9/12/14-bit、
// bypass、PAFF 或冗余图像误归为损坏；流水线支持范围仍由既有入口判断。
// 未知 profile 的上下文可能只是低层语法调用方提供的局部参数集，因此不在
// 此处声称支持或拒绝它；完整 SPS 的 validateSupported 负责支持范围。
func knownProfile(profile uint8) bool {
	switch profile {
	case 66, 77, 88, 100, 110, 122, 244:
		return true
	default:
		return false
	}
}

func validateProfileSPS(s *SPS) error {
	if s == nil {
		return fmt.Errorf("syntax: profile validation requires SPS: %w", h264.ErrInvalidData)
	}
	if !knownProfile(s.ProfileIDC) {
		return nil
	}
	legacy := s.ProfileIDC == 66 || s.ProfileIDC == 77 || s.ProfileIDC == 88
	baseline := s.ProfileIDC == 66 || s.ConstraintSetFlags[0]
	extended := s.ProfileIDC == 88 || s.ConstraintSetFlags[2]
	if legacy {
		// ChromaFormatIDC=0也允许作为手工上下文的未填值，实际语法隐含420。
		if s.ChromaFormatIDC > 1 || s.SeparateColourPlaneFlag || s.BitDepthLumaMinus8 != 0 || s.BitDepthChromaMinus8 != 0 || s.QPPPrimeYZeroTransformBypassFlag || s.SeqScalingMatrixPresentFlag {
			return fmt.Errorf("syntax: legacy profile %d cannot signal High SPS tools: %w", s.ProfileIDC, h264.ErrInvalidData)
		}
	} else {
		if s.ConstraintSetFlags[0] || s.ConstraintSetFlags[1] || s.ConstraintSetFlags[2] {
			return fmt.Errorf("syntax: High profile %d requires constraint_set0/1/2_flag=0: %w", s.ProfileIDC, h264.ErrInvalidData)
		}
		maxChroma, maxDepth := uint32(1), uint32(2)
		if s.ProfileIDC == 100 {
			maxDepth = 0
		}
		if s.ProfileIDC == 122 {
			maxChroma = 2
		}
		if s.ProfileIDC == 244 {
			maxChroma, maxDepth = 3, 6
		}
		if s.ChromaFormatIDC > maxChroma || s.BitDepthLumaMinus8 > maxDepth || s.BitDepthChromaMinus8 > maxDepth {
			return fmt.Errorf("syntax: chroma/depth %d/%d/%d exceeds profile %d: %w", s.ChromaFormatIDC, s.BitDepthLumaMinus8+8, s.BitDepthChromaMinus8+8, s.ProfileIDC, h264.ErrInvalidData)
		}
		if s.SeparateColourPlaneFlag && s.ChromaFormatIDC != 3 {
			return fmt.Errorf("syntax: separate colour planes require 4:4:4: %w", h264.ErrInvalidData)
		}
		if s.QPPPrimeYZeroTransformBypassFlag && s.ProfileIDC != 244 {
			return fmt.Errorf("syntax: transform bypass requires High 4:4:4 Predictive profile: %w", h264.ErrInvalidData)
		}
		if s.ConstraintSetFlags[3] && s.MaxNumRefFrames != 0 {
			return fmt.Errorf("syntax: Intra profile requires max_num_ref_frames=0: %w", h264.ErrInvalidData)
		}
	}
	if baseline && !s.FrameMbsOnlyFlag {
		return fmt.Errorf("syntax: Baseline constraints require frame_mbs_only_flag=1: %w", h264.ErrInvalidData)
	}
	if extended && !s.Direct8x8InferenceFlag {
		return fmt.Errorf("syntax: Extended constraints require direct_8x8_inference_flag=1: %w", h264.ErrInvalidData)
	}
	if s.FrameMbsOnlyFlag && s.MbAdaptiveFrameFieldFlag {
		return fmt.Errorf("syntax: frame-only SPS cannot enable MBAFF: %w", h264.ErrInvalidData)
	}
	if (s.ProfileIDC == 77 || s.ProfileIDC == 88 || s.ProfileIDC == 100) && s.ConstraintSetFlags[4] && !s.FrameMbsOnlyFlag {
		return fmt.Errorf("syntax: constraint_set4_flag requires frame-only coding: %w", h264.ErrInvalidData)
	}
	return nil
}

func validateProfilePPS(s *SPS, p *PPS) error {
	if err := validateProfileSPS(s); err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("syntax: profile validation requires PPS: %w", h264.ErrInvalidData)
	}
	if !knownProfile(s.ProfileIDC) {
		return nil
	}
	legacy := s.ProfileIDC == 66 || s.ProfileIDC == 77 || s.ProfileIDC == 88
	baseline := s.ProfileIDC == 66 || s.ConstraintSetFlags[0]
	main := s.ProfileIDC == 77 || s.ConstraintSetFlags[1]
	extended := s.ProfileIDC == 88 || s.ConstraintSetFlags[2]
	if legacy && p.needsOptionalBlock() {
		return fmt.Errorf("syntax: profile %d forbids PPS extension syntax: %w", s.ProfileIDC, h264.ErrInvalidData)
	}
	if (baseline || extended) && p.EntropyCodingModeFlag {
		return fmt.Errorf("syntax: Baseline/Extended constraints forbid CABAC: %w", h264.ErrInvalidData)
	}
	if baseline && (p.WeightedPredFlag || p.WeightedBipredIDC != 0) {
		return fmt.Errorf("syntax: Baseline constraints forbid weighted prediction: %w", h264.ErrInvalidData)
	}
	if p.NumSliceGroupsMinus1 > 7 {
		return fmt.Errorf("syntax: num_slice_groups_minus1 exceeds 7: %w", h264.ErrInvalidData)
	}
	if (main || !legacy) && (p.NumSliceGroupsMinus1 != 0 || p.RedundantPicCntPresentFlag) {
		return fmt.Errorf("syntax: Main/High constraints forbid FMO and redundant pictures: %w", h264.ErrInvalidData)
	}
	return nil
}

// partitioned由NAL容器提供，不能从SliceType推断；SP/SI可以使用分区，
// 但只有Extended声明允许NAL类型2..4，且IDR不能分区（7.4.1）。
func validateProfileSlice(s *SPS, p *PPS, h *SliceHeader, partitioned bool) error {
	if err := validateProfilePPS(s, p); err != nil {
		return err
	}
	if h == nil {
		return fmt.Errorf("syntax: profile validation requires slice header: %w", h264.ErrInvalidData)
	}
	if !knownProfile(s.ProfileIDC) {
		return nil
	}
	baseline := s.ProfileIDC == 66 || s.ConstraintSetFlags[0]
	main := s.ProfileIDC == 77 || s.ConstraintSetFlags[1]
	extended := s.ProfileIDC == 88
	if h.Type > SliceTypeSI || baseline && h.Type != SliceTypeI && h.Type != SliceTypeP || (main || !extended) && (h.Type == SliceTypeSP || h.Type == SliceTypeSI) {
		return fmt.Errorf("syntax: slice type %d violates profile %d constraints: %w", h.Type, s.ProfileIDC, h264.ErrInvalidData)
	}
	if (s.ProfileIDC == 77 || s.ProfileIDC == 88 || s.ProfileIDC == 100) && s.ConstraintSetFlags[5] && h.Type == SliceTypeB {
		return fmt.Errorf("syntax: constraint_set5_flag forbids B slices: %w", h264.ErrInvalidData)
	}
	if partitioned && (!extended || baseline || main || h.IDR) {
		return fmt.Errorf("syntax: data partitioning violates profile/IDR constraints: %w", h264.ErrInvalidData)
	}
	if h.RedundantPicCnt > 127 || !p.RedundantPicCntPresentFlag && h.RedundantPicCnt != 0 {
		return fmt.Errorf("syntax: redundant_pic_cnt violates PPS presence/range: %w", h264.ErrInvalidData)
	}
	if s.ProfileIDC >= 100 && s.ConstraintSetFlags[3] && (!h.IDR || h.Type != SliceTypeI) {
		return fmt.Errorf("syntax: Intra profile requires IDR I slices: %w", h264.ErrInvalidData)
	}
	return nil
}
