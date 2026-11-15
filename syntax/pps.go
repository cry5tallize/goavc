package syntax

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// PPS 是 picture parameter set 的解析结果（H.264 §7.3.2.2）。
//
// 可选的 transform_8x8/scaling-list 扩展由 more_rbsp_data() 决定是否出现。
// HasOptionalBlock 记录原始码流是否包含该扩展，从而保证 parse→serialize
// 可以逐字节复现；即使扩展里的字段全是默认值，也不会被静默省略。
type PPS struct {
	PicParameterSetID uint32
	SeqParameterSetID uint32

	EntropyCodingModeFlag                 bool
	BottomFieldPicOrderInFramePresentFlag bool

	// FMO 字段的长度和地址均以 map unit 为单位（§7.3.2.2）。
	NumSliceGroupsMinus1          uint32
	SliceGroupMapType             uint32
	RunLengthMinus1               []uint32
	TopLeft                       []uint32
	BottomRight                   []uint32
	SliceGroupChangeDirectionFlag bool
	SliceGroupChangeRateMinus1    uint32
	PicSizeInMapUnitsMinus1       uint32
	SliceGroupID                  []uint8

	NumRefIdxL0DefaultActiveMinus1 uint32
	NumRefIdxL1DefaultActiveMinus1 uint32
	WeightedPredFlag               bool
	WeightedBipredIDC              uint8

	PicInitQPMinus26    int32
	PicInitQSMinus26    int32
	ChromaQPIndexOffset int32

	DeblockingFilterControlPresentFlag bool
	ConstrainedIntraPredFlag           bool
	RedundantPicCntPresentFlag         bool

	HasOptionalBlock            bool
	Transform8x8ModeFlag        bool
	PicScalingMatrixPresentFlag bool
	PicScalingListPresentFlag   [12]bool
	PicScalingList              [12]*ScalingList
	SecondChromaQPIndexOffset   int32
}

// ParsePPS 解析完整的 PPS RBSP。
//
// sps 用于确定 scaling list 的数量（4:2:0/4:2:2 为 8，4:4:4 为 12），
// 因此不能为 nil。FMO 0..6 的参数按 SPS 图像尺寸校验。
func ParsePPS(rbsp []byte, sps *SPS) (*PPS, error) {
	if sps == nil {
		return nil, fmt.Errorf("syntax: parse pps requires an sps context: %w", h264.ErrInvalidData)
	}
	if sps.BitDepthLumaMinus8 > 6 {
		return nil, fmt.Errorf("syntax: invalid SPS bit depth: %w", h264.ErrInvalidData)
	}
	r := bitstream.NewReader(rbsp)
	p := &PPS{}
	var err error

	if p.PicParameterSetID, err = readUE(r, "pic_parameter_set_id"); err != nil {
		return nil, err
	}
	if p.PicParameterSetID > 255 {
		return nil, fmt.Errorf("syntax: pic_parameter_set_id = %d exceeds 255: %w",
			p.PicParameterSetID, h264.ErrInvalidData)
	}
	if p.SeqParameterSetID, err = readUE(r, "seq_parameter_set_id"); err != nil {
		return nil, err
	}
	if p.SeqParameterSetID > 31 {
		return nil, fmt.Errorf("syntax: seq_parameter_set_id = %d exceeds 31: %w",
			p.SeqParameterSetID, h264.ErrInvalidData)
	}
	if p.SeqParameterSetID != sps.SeqParameterSetID {
		return nil, fmt.Errorf("syntax: PPS references SPS %d, got SPS %d: %w", p.SeqParameterSetID, sps.SeqParameterSetID, h264.ErrInvalidData)
	}
	if p.EntropyCodingModeFlag, err = readFlag(r, "entropy_coding_mode_flag"); err != nil {
		return nil, err
	}
	if p.BottomFieldPicOrderInFramePresentFlag, err = readFlag(r, "bottom_field_pic_order_in_frame_present_flag"); err != nil {
		return nil, err
	}
	if p.NumSliceGroupsMinus1, err = readUE(r, "num_slice_groups_minus1"); err != nil {
		return nil, err
	}
	if err := p.parseFMO(r, sps); err != nil {
		return nil, err
	}
	if p.NumRefIdxL0DefaultActiveMinus1, err = readUE(r, "num_ref_idx_l0_default_active_minus1"); err != nil {
		return nil, err
	}
	if p.NumRefIdxL1DefaultActiveMinus1, err = readUE(r, "num_ref_idx_l1_default_active_minus1"); err != nil {
		return nil, err
	}
	if p.NumRefIdxL0DefaultActiveMinus1 > 31 || p.NumRefIdxL1DefaultActiveMinus1 > 31 {
		return nil, fmt.Errorf("syntax: num_ref_idx default active minus1 exceeds 31: %w", h264.ErrInvalidData)
	}
	if p.WeightedPredFlag, err = readFlag(r, "weighted_pred_flag"); err != nil {
		return nil, err
	}
	weightedBipred, err := readBits(r, 2, "weighted_bipred_idc")
	if err != nil {
		return nil, err
	}
	if weightedBipred > 2 {
		return nil, fmt.Errorf("syntax: weighted_bipred_idc = %d exceeds 2: %w",
			weightedBipred, h264.ErrInvalidData)
	}
	p.WeightedBipredIDC = uint8(weightedBipred)
	if p.PicInitQPMinus26, err = readSE(r, "pic_init_qp_minus26"); err != nil {
		return nil, err
	}
	if err := validateQPOffset("pic_init_qp_minus26", p.PicInitQPMinus26, -26-6*int32(sps.BitDepthLumaMinus8), 25); err != nil {
		return nil, err
	}
	if p.PicInitQSMinus26, err = readSE(r, "pic_init_qs_minus26"); err != nil {
		return nil, err
	}
	if err := validateQPOffset("pic_init_qs_minus26", p.PicInitQSMinus26, -26, 25); err != nil {
		return nil, err
	}
	if p.ChromaQPIndexOffset, err = readSE(r, "chroma_qp_index_offset"); err != nil {
		return nil, err
	}
	if err := validateQPOffset("chroma_qp_index_offset", p.ChromaQPIndexOffset, -12, 12); err != nil {
		return nil, err
	}
	if p.DeblockingFilterControlPresentFlag, err = readFlag(r, "deblocking_filter_control_present_flag"); err != nil {
		return nil, err
	}
	if p.ConstrainedIntraPredFlag, err = readFlag(r, "constrained_intra_pred_flag"); err != nil {
		return nil, err
	}
	if p.RedundantPicCntPresentFlag, err = readFlag(r, "redundant_pic_cnt_present_flag"); err != nil {
		return nil, err
	}

	if p.HasOptionalBlock, err = moreRBSPData(r); err != nil {
		return nil, err
	}
	if p.HasOptionalBlock {
		if p.Transform8x8ModeFlag, err = readFlag(r, "transform_8x8_mode_flag"); err != nil {
			return nil, err
		}
		if p.PicScalingMatrixPresentFlag, err = readFlag(r, "pic_scaling_matrix_present_flag"); err != nil {
			return nil, err
		}
		if p.PicScalingMatrixPresentFlag {
			count := scalingListCount(sps, p.Transform8x8ModeFlag)
			for i := 0; i < count; i++ {
				present, err := readFlag(r, "pic_scaling_list_present_flag")
				if err != nil {
					return nil, err
				}
				p.PicScalingListPresentFlag[i] = present
				if !present {
					continue
				}
				size := 4
				if i >= 6 {
					size = 8
				}
				list, err := ParseScalingList(r, size)
				if err != nil {
					return nil, fmt.Errorf("syntax: pps pic_scaling_list[%d]: %w", i, err)
				}
				p.PicScalingList[i] = list
			}
		}
		if p.SecondChromaQPIndexOffset, err = readSE(r, "second_chroma_qp_index_offset"); err != nil {
			return nil, err
		}
		if err := validateQPOffset("second_chroma_qp_index_offset", p.SecondChromaQPIndexOffset, -12, 12); err != nil {
			return nil, err
		}
	} else {
		// 扩展不存在时，规范规定 second_chroma_qp_index_offset 等于
		// chroma_qp_index_offset。
		p.SecondChromaQPIndexOffset = p.ChromaQPIndexOffset
	}

	if err := consumeRBSPTrailingBits(r); err != nil {
		return nil, fmt.Errorf("syntax: pps trailing bits: %w", err)
	}
	if err := validateProfilePPS(sps, p); err != nil {
		return nil, err
	}
	return p, nil
}

// WriteTo 写出 PPS 语法字段，不包含 rbsp_trailing_bits。
func (p *PPS) WriteTo(w *bitstream.Writer, sps *SPS) error {
	if err := p.validateForWrite(sps); err != nil {
		return err
	}
	if err := writeUE(w, p.PicParameterSetID, "pic_parameter_set_id"); err != nil {
		return err
	}
	if err := writeUE(w, p.SeqParameterSetID, "seq_parameter_set_id"); err != nil {
		return err
	}
	if err := writeFlag(w, p.EntropyCodingModeFlag); err != nil {
		return err
	}
	if err := writeFlag(w, p.BottomFieldPicOrderInFramePresentFlag); err != nil {
		return err
	}
	if err := writeUE(w, p.NumSliceGroupsMinus1, "num_slice_groups_minus1"); err != nil {
		return err
	}
	if err := p.writeFMO(w); err != nil {
		return err
	}
	if err := writeUE(w, p.NumRefIdxL0DefaultActiveMinus1, "num_ref_idx_l0_default_active_minus1"); err != nil {
		return err
	}
	if err := writeUE(w, p.NumRefIdxL1DefaultActiveMinus1, "num_ref_idx_l1_default_active_minus1"); err != nil {
		return err
	}
	if err := writeFlag(w, p.WeightedPredFlag); err != nil {
		return err
	}
	if err := writeBits(w, 2, uint64(p.WeightedBipredIDC), "weighted_bipred_idc"); err != nil {
		return err
	}
	if err := writeSE(w, p.PicInitQPMinus26, "pic_init_qp_minus26"); err != nil {
		return err
	}
	if err := writeSE(w, p.PicInitQSMinus26, "pic_init_qs_minus26"); err != nil {
		return err
	}
	if err := writeSE(w, p.ChromaQPIndexOffset, "chroma_qp_index_offset"); err != nil {
		return err
	}
	if err := writeFlag(w, p.DeblockingFilterControlPresentFlag); err != nil {
		return err
	}
	if err := writeFlag(w, p.ConstrainedIntraPredFlag); err != nil {
		return err
	}
	if err := writeFlag(w, p.RedundantPicCntPresentFlag); err != nil {
		return err
	}

	if p.needsOptionalBlock() {
		if err := writeFlag(w, p.Transform8x8ModeFlag); err != nil {
			return err
		}
		if err := writeFlag(w, p.PicScalingMatrixPresentFlag); err != nil {
			return err
		}
		if p.PicScalingMatrixPresentFlag {
			count := scalingListCount(sps, p.Transform8x8ModeFlag)
			for i := 0; i < count; i++ {
				present := p.PicScalingListPresentFlag[i]
				if err := writeFlag(w, present); err != nil {
					return err
				}
				if !present {
					continue
				}
				list := p.PicScalingList[i]
				if list == nil {
					return fmt.Errorf("syntax: pps pic_scaling_list_present_flag[%d] set but list is nil", i)
				}
				expectedSize := 4
				if i >= 6 {
					expectedSize = 8
				}
				if list.Size != expectedSize {
					return fmt.Errorf("syntax: pps pic_scaling_list[%d] size %d, expected %d",
						i, list.Size, expectedSize)
				}
				if err := list.WriteTo(w); err != nil {
					return fmt.Errorf("syntax: pps pic_scaling_list[%d]: %w", i, err)
				}
			}
		}
		if err := writeSE(w, p.SecondChromaQPIndexOffset, "second_chroma_qp_index_offset"); err != nil {
			return err
		}
	}
	return nil
}

// MarshalRBSP 序列化完整 PPS RBSP（含 rbsp_trailing_bits）。
func (p *PPS) MarshalRBSP(sps *SPS) ([]byte, error) {
	w := bitstream.NewWriter()
	if err := p.WriteTo(w, sps); err != nil {
		return nil, err
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		return nil, fmt.Errorf("syntax: pps trailing bits: %w", err)
	}
	return w.Bytes(), nil
}

// needsOptionalBlock 判断是否写出扩展块。
//
// HasOptionalBlock 保留原始码流的选择；如果调用方手工设置了扩展字段，
// 即使没有置 HasOptionalBlock，也会写出扩展，避免字段被静默丢弃。
func (p *PPS) needsOptionalBlock() bool {
	return p.HasOptionalBlock ||
		p.Transform8x8ModeFlag ||
		p.PicScalingMatrixPresentFlag ||
		p.SecondChromaQPIndexOffset != p.ChromaQPIndexOffset
}

func (p *PPS) validateForWrite(sps *SPS) error {
	if p == nil {
		return fmt.Errorf("syntax: nil pps: %w", h264.ErrInvalidData)
	}
	if sps == nil {
		return fmt.Errorf("syntax: write pps requires an sps context: %w", h264.ErrInvalidData)
	}
	if sps.BitDepthLumaMinus8 > 6 {
		return fmt.Errorf("syntax: invalid SPS bit depth: %w", h264.ErrInvalidData)
	}
	if p.PicParameterSetID > 255 {
		return fmt.Errorf("syntax: pic_parameter_set_id %d exceeds 255: %w",
			p.PicParameterSetID, h264.ErrInvalidData)
	}
	if p.SeqParameterSetID > 31 {
		return fmt.Errorf("syntax: seq_parameter_set_id %d exceeds 31: %w",
			p.SeqParameterSetID, h264.ErrInvalidData)
	}
	if p.SeqParameterSetID != sps.SeqParameterSetID {
		return fmt.Errorf("syntax: PPS references SPS %d, got SPS %d: %w", p.SeqParameterSetID, sps.SeqParameterSetID, h264.ErrInvalidData)
	}
	if err := p.validateFMO(sps); err != nil {
		return err
	}
	if p.NumRefIdxL0DefaultActiveMinus1 > 31 || p.NumRefIdxL1DefaultActiveMinus1 > 31 {
		return fmt.Errorf("syntax: num_ref_idx default active minus1 exceeds 31: %w", h264.ErrInvalidData)
	}
	if p.WeightedBipredIDC > 2 {
		return fmt.Errorf("syntax: weighted_bipred_idc %d exceeds 2: %w", p.WeightedBipredIDC, h264.ErrInvalidData)
	}
	if err := validateQPOffset("pic_init_qp_minus26", p.PicInitQPMinus26, -26-6*int32(sps.BitDepthLumaMinus8), 25); err != nil {
		return err
	}
	if err := validateQPOffset("pic_init_qs_minus26", p.PicInitQSMinus26, -26, 25); err != nil {
		return err
	}
	if err := validateQPOffset("chroma_qp_index_offset", p.ChromaQPIndexOffset, -12, 12); err != nil {
		return err
	}
	if p.needsOptionalBlock() {
		if err := validateQPOffset("second_chroma_qp_index_offset", p.SecondChromaQPIndexOffset, -12, 12); err != nil {
			return err
		}
	}
	if err := validateProfilePPS(sps, p); err != nil {
		return err
	}
	return validatePPSScalingLists(sps, p)
}

// scalingListCount 仅在启用8×8变换时追加2/6个8×8列表（Spec: 7.3.2.2）。
func scalingListCount(sps *SPS, transform8 bool) int {
	if !transform8 {
		return 6
	}
	if sps.effectiveChromaFormatIDC() == 3 {
		return 12
	}
	return 8
}

// validateQPOffset 检查 se(v) 字段的语义范围。
func validateQPOffset(name string, value int32, minValue, maxValue int32) error {
	if value < minValue || value > maxValue {
		return fmt.Errorf("syntax: %s = %d out of range %d..%d: %w",
			name, value, minValue, maxValue, h264.ErrInvalidData)
	}
	return nil
}
