package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// SliceType 是 slice_type 归一化后的类型（5 种基本类型）。
type SliceType uint8

const (
	SliceTypeP SliceType = iota
	SliceTypeB
	SliceTypeI
	SliceTypeSP
	SliceTypeSI
)

var sliceTypeNames = [...]string{"P", "B", "I", "SP", "SI"}

// String 返回稳定的类型名称。
func (t SliceType) String() string {
	if int(t) >= len(sliceTypeNames) {
		return "unknown"
	}
	return sliceTypeNames[t]
}

// sliceTypeFromCode 把 0..9 的 slice_type 映射为基本类型。
//
// 5..9 表示"所有 slice 都是同一类型"，与 0..4 的映射相同（§7.4.3）。
func sliceTypeFromCode(code uint32) (SliceType, bool) {
	if code > 9 {
		return 0, false
	}
	return SliceType(code % 5), true
}

// SliceHeader 是 slice header 的解析结果（规范 7.3.3）。
//
// 保留 I/P/B 参考列表、MMCO 和加权预测的完整值，供 M3 图像管理与预测消费。
//
// Has* 标志用于区分"字段不存在"与"字段值为 0"。
type SliceHeader struct {
	FirstMBInSlice    uint32
	SliceTypeCode     uint32
	Type              SliceType
	PicParameterSetID uint32

	FrameNum        uint32
	FieldPicFlag    bool
	BottomFieldFlag bool

	IDR      bool
	IDRPicID uint32

	HasPicOrderCntLsb         bool
	PicOrderCntLsb            uint32
	HasDeltaPicOrderCntBottom bool
	DeltaPicOrderCntBottom    int32
	HasDeltaPicOrderCnt       bool
	DeltaPicOrderCnt          [2]int32

	RedundantPicCnt uint32

	// 参考帧数量。P 切片只使用 list0；缺省值来自 PPS。
	NumRefIdxActiveOverrideFlag bool
	NumRefIdxL0ActiveMinus1     uint32
	NumRefIdxL1ActiveMinus1     uint32

	// 参考列表重排标志与原始命令。
	RefPicListReorderingL0 bool
	RefPicListReorderingL1 bool
	RefListModifications   [2][]RefListModification

	// 参考帧标记（nal_ref_idc != 0 时存在）。
	NoOutputOfPriorPicsFlag bool
	LongTermReferenceFlag   bool
	AdaptiveRefPicMarking   bool
	MMCOCount               int
	MemoryManagement        []MMCO

	// HasPredWeightTable 表示 header 携带 pred_weight_table()。
	// PredictionWeights 保存分母、默认权重和显式权重。
	HasPredWeightTable bool
	PredictionWeights  *WeightTable

	// CABAC 初始化（仅 CABAC 且非 I/SI 切片存在）。
	CabacInitIDC uint32

	// B 切片的 direct_spatial_mv_pred_flag，选择 spatial 或 temporal direct。
	DirectSpatialMVPredFlag bool

	// 量化参数。SliceQPDelta 是码流中的 se(v)，SliceQP 是加上
	// PPS pic_init_qp_minus26 与 26 后的实际 QP Y。
	SliceQPDelta int32
	SliceQP      int

	// 去块参数。PPS 未携带 deblocking_filter_control_present_flag 时，
	// DisableDeblockingFilterIDC 保持 0（滤波开启）。
	DisableDeblockingFilterIDC uint32
	SliceAlphaC0OffsetDiv2     int32
	SliceBetaOffsetDiv2        int32

	// HeaderBits 是 slice header 占用的比特数，slice data 从该位置开始。
	// 调用方在同一 RBSP 上新建 Reader 并 SkipBits(HeaderBits) 即可。
	HeaderBits uint64
}

// ParseSliceHeader 解析 slice header 的公共前缀字段。
//
// rbsp 是 slice NAL 的 RBSP（去掉 NAL header 与防竞争字节），可以包含
// 后续 slice data；本函数只消费 header 字段，不校验尾位。
//
// ppsByID 用于按 pic_parameter_set_id 查找 PPS：该字段位于 header 前部，
// 解析器不能依赖调用方预先知道 PPS。idr 由 NAL unit type 是否为 5 决定。
func ParseSliceHeader(rbsp []byte, sps *SPS, ppsByID map[uint32]*PPS, idr bool) (*SliceHeader, error) {
	return ParseSliceHeaderEx(rbsp, sps, ppsByID, idr, 3)
}

// ParseSliceHeaderEx 解析完整 slice header，并显式接收 nal_ref_idc。
//
// nal_ref_idc 决定 dec_ref_pic_marking() 是否存在：非参考图像的 slice
// 没有该语法块。旧入口 ParseSliceHeader 默认按参考图像处理，仅用于
// AU 边界与语法层测试；解码器必须使用本函数。
func ParseSliceHeaderEx(rbsp []byte, sps *SPS, ppsByID map[uint32]*PPS, idr bool, nalRefIDC uint8) (*SliceHeader, error) {
	if sps == nil {
		return nil, fmt.Errorf("syntax: parse slice header requires an sps")
	}
	if ppsByID == nil {
		return nil, fmt.Errorf("syntax: parse slice header requires a pps lookup")
	}
	r := bitstream.NewReader(rbsp)
	h := &SliceHeader{IDR: idr}
	var err error

	if h.FirstMBInSlice, err = readUE(r, "first_mb_in_slice"); err != nil {
		return nil, err
	}
	if h.SliceTypeCode, err = readUE(r, "slice_type"); err != nil {
		return nil, err
	}
	typ, ok := sliceTypeFromCode(h.SliceTypeCode)
	if !ok {
		return nil, fmt.Errorf("syntax: slice_type = %d exceeds 9: %w", h.SliceTypeCode, h264.ErrInvalidData)
	}
	h.Type = typ
	if typ == SliceTypeSP || typ == SliceTypeSI {
		return nil, fmt.Errorf("syntax: slice type %s is not supported in M1: %w", typ, h264.ErrUnsupported)
	}

	if h.PicParameterSetID, err = readUE(r, "pic_parameter_set_id"); err != nil {
		return nil, err
	}
	if h.PicParameterSetID > 255 {
		return nil, fmt.Errorf("syntax: pic_parameter_set_id = %d exceeds 255: %w",
			h.PicParameterSetID, h264.ErrInvalidData)
	}
	pps := ppsByID[h.PicParameterSetID]
	if pps == nil {
		return nil, fmt.Errorf("syntax: slice references unknown pps id %d: %w",
			h.PicParameterSetID, h264.ErrInvalidData)
	}
	if pps.SeqParameterSetID != sps.SeqParameterSetID {
		return nil, fmt.Errorf("syntax: pps %d references sps %d, got sps %d: %w",
			pps.PicParameterSetID, pps.SeqParameterSetID, sps.SeqParameterSetID, h264.ErrInvalidData)
	}

	frameNumBits := uint(sps.Log2MaxFrameNumMinus4) + 4
	frameNum, err := readBits(r, frameNumBits, "frame_num")
	if err != nil {
		return nil, err
	}
	h.FrameNum = uint32(frameNum)

	if !sps.FrameMbsOnlyFlag {
		if h.FieldPicFlag, err = readFlag(r, "field_pic_flag"); err != nil {
			return nil, err
		}
		if h.FieldPicFlag {
			if h.BottomFieldFlag, err = readFlag(r, "bottom_field_flag"); err != nil {
				return nil, err
			}
		}
	}

	if idr {
		if h.IDRPicID, err = readUE(r, "idr_pic_id"); err != nil {
			return nil, err
		}
	}

	switch sps.PicOrderCntType {
	case 0:
		pocBits := uint(sps.Log2MaxPicOrderCntLsbMinus4) + 4
		poc, err := readBits(r, pocBits, "pic_order_cnt_lsb")
		if err != nil {
			return nil, err
		}
		h.HasPicOrderCntLsb = true
		h.PicOrderCntLsb = uint32(poc)
		if pps.BottomFieldPicOrderInFramePresentFlag && !h.FieldPicFlag {
			h.HasDeltaPicOrderCntBottom = true
			if h.DeltaPicOrderCntBottom, err = readSE(r, "delta_pic_order_cnt_bottom"); err != nil {
				return nil, err
			}
		}
	case 1:
		if !sps.DeltaPicOrderAlwaysZeroFlag {
			h.HasDeltaPicOrderCnt = true
			if h.DeltaPicOrderCnt[0], err = readSE(r, "delta_pic_order_cnt[0]"); err != nil {
				return nil, err
			}
			if pps.BottomFieldPicOrderInFramePresentFlag && !h.FieldPicFlag {
				if h.DeltaPicOrderCnt[1], err = readSE(r, "delta_pic_order_cnt[1]"); err != nil {
					return nil, err
				}
			}
		}
	case 2:
		// POC type 2 在 slice header 中没有 POC 字段。
	}

	if pps.RedundantPicCntPresentFlag {
		if h.RedundantPicCnt, err = readUE(r, "redundant_pic_cnt"); err != nil {
			return nil, err
		}
		if h.RedundantPicCnt > 127 {
			return nil, fmt.Errorf("syntax: redundant_pic_cnt = %d exceeds 127: %w",
				h.RedundantPicCnt, h264.ErrInvalidData)
		}
	}

	// 参考帧数量默认取 PPS，P/B 切片可用 override_flag 覆盖。
	h.NumRefIdxL0ActiveMinus1 = pps.NumRefIdxL0DefaultActiveMinus1
	h.NumRefIdxL1ActiveMinus1 = pps.NumRefIdxL1DefaultActiveMinus1
	if typ == SliceTypeB {
		if h.DirectSpatialMVPredFlag, err = readFlag(r, "direct_spatial_mv_pred_flag"); err != nil {
			return nil, err
		}
	}
	if typ == SliceTypeP || typ == SliceTypeB {
		if h.NumRefIdxActiveOverrideFlag, err = readFlag(r, "num_ref_idx_active_override_flag"); err != nil {
			return nil, err
		}
		if h.NumRefIdxActiveOverrideFlag {
			if h.NumRefIdxL0ActiveMinus1, err = readUE(r, "num_ref_idx_l0_active_minus1"); err != nil {
				return nil, err
			}
			if typ == SliceTypeB {
				if h.NumRefIdxL1ActiveMinus1, err = readUE(r, "num_ref_idx_l1_active_minus1"); err != nil {
					return nil, err
				}
			}
		}
		if h.NumRefIdxL0ActiveMinus1 > 31 || h.NumRefIdxL1ActiveMinus1 > 31 {
			return nil, fmt.Errorf("syntax: active ref idx minus1 exceeds 31: %w", h264.ErrInvalidData)
		}
		// ref_pic_list_reordering()：保留完整命令供 P/B 列表构造消费。
		if h.RefPicListReorderingL0, err = readFlag(r, "ref_pic_list_reordering_flag_l0"); err != nil {
			return nil, err
		}
		if h.RefPicListReorderingL0 {
			if h.RefListModifications[0], err = parseRefPicListReordering(r); err != nil {
				return nil, err
			}
		}
		if typ == SliceTypeB {
			if h.RefPicListReorderingL1, err = readFlag(r, "ref_pic_list_reordering_flag_l1"); err != nil {
				return nil, err
			}
			if h.RefPicListReorderingL1 {
				if h.RefListModifications[1], err = parseRefPicListReordering(r); err != nil {
					return nil, err
				}
			}
		}
	}

	// pred_weight_table()：保留默认值、显式权重和偏移供预测消费。
	if (pps.WeightedPredFlag && typ == SliceTypeP) ||
		(pps.WeightedBipredIDC == 1 && typ == SliceTypeB) {
		h.HasPredWeightTable = true
		if h.PredictionWeights, err = parsePredWeightTable(r, sps, typ, h.NumRefIdxL0ActiveMinus1, h.NumRefIdxL1ActiveMinus1); err != nil {
			return nil, err
		}
	}

	if nalRefIDC != 0 {
		if idr {
			if h.NoOutputOfPriorPicsFlag, err = readFlag(r, "no_output_of_prior_pics_flag"); err != nil {
				return nil, err
			}
			if h.LongTermReferenceFlag, err = readFlag(r, "long_term_reference_flag"); err != nil {
				return nil, err
			}
		} else {
			if h.AdaptiveRefPicMarking, err = readFlag(r, "adaptive_ref_pic_marking_mode_flag"); err != nil {
				return nil, err
			}
			if h.AdaptiveRefPicMarking {
				if h.MemoryManagement, err = parseDecRefPicMarking(r); err != nil {
					return nil, err
				}
				h.MMCOCount = len(h.MemoryManagement)
			}
		}
	}

	if pps.EntropyCodingModeFlag && typ != SliceTypeI && typ != SliceTypeSI {
		if h.CabacInitIDC, err = readUE(r, "cabac_init_idc"); err != nil {
			return nil, err
		}
		if h.CabacInitIDC > 2 {
			return nil, fmt.Errorf("syntax: cabac_init_idc = %d exceeds 2: %w",
				h.CabacInitIDC, h264.ErrInvalidData)
		}
	}

	if h.SliceQPDelta, err = readSE(r, "slice_qp_delta"); err != nil {
		return nil, err
	}
	// Spec: 7.4.3。语法QPY可为负；有效QP'偏移只在变换量化边界应用。
	qp := int64(26) + int64(pps.PicInitQPMinus26) + int64(h.SliceQPDelta)
	if sps.BitDepthLumaMinus8 > 6 || qp < -6*int64(sps.BitDepthLumaMinus8) || qp > 51 {
		return nil, fmt.Errorf("syntax: slice QPY %d out of range: %w", qp, h264.ErrInvalidData)
	}
	h.SliceQP = int(qp)

	if pps.DeblockingFilterControlPresentFlag {
		if h.DisableDeblockingFilterIDC, err = readUE(r, "disable_deblocking_filter_idc"); err != nil {
			return nil, err
		}
		if h.DisableDeblockingFilterIDC > 2 {
			return nil, fmt.Errorf("syntax: disable_deblocking_filter_idc = %d exceeds 2: %w",
				h.DisableDeblockingFilterIDC, h264.ErrInvalidData)
		}
		if h.DisableDeblockingFilterIDC != 1 {
			if h.SliceAlphaC0OffsetDiv2, err = readSE(r, "slice_alpha_c0_offset_div2"); err != nil {
				return nil, err
			}
			if h.SliceAlphaC0OffsetDiv2 < -6 || h.SliceAlphaC0OffsetDiv2 > 6 {
				return nil, fmt.Errorf("syntax: slice_alpha_c0_offset_div2 = %d out of range -6..6: %w",
					h.SliceAlphaC0OffsetDiv2, h264.ErrInvalidData)
			}
			if h.SliceBetaOffsetDiv2, err = readSE(r, "slice_beta_offset_div2"); err != nil {
				return nil, err
			}
			if h.SliceBetaOffsetDiv2 < -6 || h.SliceBetaOffsetDiv2 > 6 {
				return nil, fmt.Errorf("syntax: slice_beta_offset_div2 = %d out of range -6..6: %w",
					h.SliceBetaOffsetDiv2, h264.ErrInvalidData)
			}
		}
	}

	h.HeaderBits = r.BitsRead()
	return h, nil
}
