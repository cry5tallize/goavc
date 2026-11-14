package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// WriteTo 写出 SPS 的语法字段，不包含 rbsp_trailing_bits。
//
// 写出前会执行结构与支持范围校验；任何会产出非法或不支持码流的配置
// 都返回错误，而不是写出半个参数集。
func (s *SPS) WriteTo(w *bitstream.Writer) error {
	if err := s.validateForWrite(); err != nil {
		return err
	}

	if err := writeBits(w, 8, uint64(s.ProfileIDC), "profile_idc"); err != nil {
		return err
	}
	var constraints uint64
	for i := 0; i < 6; i++ {
		if s.ConstraintSetFlags[i] {
			constraints |= 1 << (7 - i)
		}
	}
	constraints |= uint64(s.ReservedZero2Bits)
	if err := writeBits(w, 8, constraints, "constraint_set flags"); err != nil {
		return err
	}
	if err := writeBits(w, 8, uint64(s.LevelIDC), "level_idc"); err != nil {
		return err
	}
	if err := writeUE(w, s.SeqParameterSetID, "seq_parameter_set_id"); err != nil {
		return err
	}

	if usesHighProfileSyntax(s.ProfileIDC) {
		if err := writeUE(w, s.ChromaFormatIDC, "chroma_format_idc"); err != nil {
			return err
		}
		if s.ChromaFormatIDC == 3 {
			if err := writeFlag(w, s.SeparateColourPlaneFlag); err != nil {
				return err
			}
		}
		if err := writeUE(w, s.BitDepthLumaMinus8, "bit_depth_luma_minus8"); err != nil {
			return err
		}
		if err := writeUE(w, s.BitDepthChromaMinus8, "bit_depth_chroma_minus8"); err != nil {
			return err
		}
		if err := writeFlag(w, s.QPPPrimeYZeroTransformBypassFlag); err != nil {
			return err
		}
		if err := writeFlag(w, s.SeqScalingMatrixPresentFlag); err != nil {
			return err
		}
		if s.SeqScalingMatrixPresentFlag {
			count := 8
			if s.ChromaFormatIDC == 3 {
				count = 12
			}
			for i := 0; i < count; i++ {
				present := s.SeqScalingListPresentFlag[i]
				if err := writeFlag(w, present); err != nil {
					return err
				}
				if !present {
					continue
				}
				list := s.SeqScalingList[i]
				if list == nil {
					return fmt.Errorf("syntax: sps seq_scaling_list_present_flag[%d] set but list is nil", i)
				}
				expectedSize := 4
				if i >= 6 {
					expectedSize = 8
				}
				if list.Size != expectedSize {
					return fmt.Errorf("syntax: sps seq_scaling_list[%d] size %d, expected %d",
						i, list.Size, expectedSize)
				}
				if err := list.WriteTo(w); err != nil {
					return fmt.Errorf("syntax: sps seq_scaling_list[%d]: %w", i, err)
				}
			}
		}
	}

	if err := writeUE(w, s.Log2MaxFrameNumMinus4, "log2_max_frame_num_minus4"); err != nil {
		return err
	}
	if err := writeUE(w, s.PicOrderCntType, "pic_order_cnt_type"); err != nil {
		return err
	}
	switch s.PicOrderCntType {
	case 0:
		if err := writeUE(w, s.Log2MaxPicOrderCntLsbMinus4, "log2_max_pic_order_cnt_lsb_minus4"); err != nil {
			return err
		}
	case 1:
		if err := writeFlag(w, s.DeltaPicOrderAlwaysZeroFlag); err != nil {
			return err
		}
		if err := writeSE(w, s.OffsetForNonRefPic, "offset_for_non_ref_pic"); err != nil {
			return err
		}
		if err := writeSE(w, s.OffsetForTopToBottomField, "offset_for_top_to_bottom_field"); err != nil {
			return err
		}
		if err := writeUE(w, s.NumRefFramesInPicOrderCntCycle, "num_ref_frames_in_pic_order_cnt_cycle"); err != nil {
			return err
		}
		for i, offset := range s.OffsetForRefFrame {
			if err := writeSE(w, offset, "offset_for_ref_frame"); err != nil {
				return fmt.Errorf("syntax: offset_for_ref_frame[%d]: %w", i, err)
			}
		}
	}

	if err := writeUE(w, s.MaxNumRefFrames, "max_num_ref_frames"); err != nil {
		return err
	}
	if err := writeFlag(w, s.GapsInFrameNumValueAllowedFlag); err != nil {
		return err
	}
	if err := writeUE(w, s.PicWidthInMbsMinus1, "pic_width_in_mbs_minus1"); err != nil {
		return err
	}
	if err := writeUE(w, s.PicHeightInMapUnitsMinus1, "pic_height_in_map_units_minus1"); err != nil {
		return err
	}
	if err := writeFlag(w, s.FrameMbsOnlyFlag); err != nil {
		return err
	}
	if !s.FrameMbsOnlyFlag {
		if err := writeFlag(w, s.MbAdaptiveFrameFieldFlag); err != nil {
			return err
		}
	}
	if err := writeFlag(w, s.Direct8x8InferenceFlag); err != nil {
		return err
	}
	if err := writeFlag(w, s.FrameCroppingFlag); err != nil {
		return err
	}
	if s.FrameCroppingFlag {
		if err := writeUE(w, s.FrameCropLeftOffset, "frame_crop_left_offset"); err != nil {
			return err
		}
		if err := writeUE(w, s.FrameCropRightOffset, "frame_crop_right_offset"); err != nil {
			return err
		}
		if err := writeUE(w, s.FrameCropTopOffset, "frame_crop_top_offset"); err != nil {
			return err
		}
		if err := writeUE(w, s.FrameCropBottomOffset, "frame_crop_bottom_offset"); err != nil {
			return err
		}
	}
	if err := writeFlag(w, s.VUIParametersPresentFlag); err != nil {
		return err
	}
	if s.VUIParametersPresentFlag {
		if err := writeVUIParameters(w, s.VUI); err != nil {
			return fmt.Errorf("syntax: sps vui_parameters: %w", err)
		}
	}
	return nil
}

// MarshalRBSP 序列化完整 SPS RBSP（含 rbsp_trailing_bits）。
func (s *SPS) MarshalRBSP() ([]byte, error) {
	w := bitstream.NewWriter()
	if err := s.WriteTo(w); err != nil {
		return nil, err
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		return nil, fmt.Errorf("syntax: sps trailing bits: %w", err)
	}
	return w.Bytes(), nil
}

// validateForWrite 校验结构、支持范围与依赖 SPS 上下文的 Annex A/E 语义。
func (s *SPS) validateForWrite() error {
	if s == nil {
		return fmt.Errorf("syntax: nil sps")
	}
	if s.ReservedZero2Bits != 0 {
		return fmt.Errorf("syntax: sps reserved_zero_2bits must be 0: %w", h264.ErrInvalidData)
	}
	if s.SeqParameterSetID > 31 {
		return fmt.Errorf("syntax: seq_parameter_set_id %d exceeds 31: %w", s.SeqParameterSetID, h264.ErrInvalidData)
	}
	if s.Log2MaxFrameNumMinus4 > 12 {
		return fmt.Errorf("syntax: log2_max_frame_num_minus4 %d exceeds 12: %w",
			s.Log2MaxFrameNumMinus4, h264.ErrInvalidData)
	}
	switch s.PicOrderCntType {
	case 0:
		if s.Log2MaxPicOrderCntLsbMinus4 > 12 {
			return fmt.Errorf("syntax: log2_max_pic_order_cnt_lsb_minus4 %d exceeds 12: %w",
				s.Log2MaxPicOrderCntLsbMinus4, h264.ErrInvalidData)
		}
	case 1:
		if s.NumRefFramesInPicOrderCntCycle > 255 {
			return fmt.Errorf("syntax: num_ref_frames_in_pic_order_cnt_cycle %d exceeds 255: %w",
				s.NumRefFramesInPicOrderCntCycle, h264.ErrInvalidData)
		}
		if len(s.OffsetForRefFrame) != int(s.NumRefFramesInPicOrderCntCycle) {
			return fmt.Errorf("syntax: offset_for_ref_frame length %d != cycle %d",
				len(s.OffsetForRefFrame), s.NumRefFramesInPicOrderCntCycle)
		}
	case 2:
	default:
		return fmt.Errorf("syntax: pic_order_cnt_type %d exceeds 2: %w", s.PicOrderCntType, h264.ErrInvalidData)
	}
	if usesHighProfileSyntax(s.ProfileIDC) {
		if s.ChromaFormatIDC > 3 {
			return fmt.Errorf("syntax: chroma_format_idc %d exceeds 3: %w", s.ChromaFormatIDC, h264.ErrInvalidData)
		}
		if s.BitDepthLumaMinus8 > 6 || s.BitDepthChromaMinus8 > 6 {
			return fmt.Errorf("syntax: bit depth minus8 exceeds 6: %w", h264.ErrInvalidData)
		}
	}
	if err := s.validateSupported(); err != nil {
		return err
	}
	return s.validateVUIForSPS()
}
