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

// SliceHeader 是 slice header 的最小解析子集，仅覆盖 Access Unit 边界判定
// 与后续 POC 初始化需要的字段。M2 会在此基础上扩展参考列表 / 加权预测 /
// 去块参数等字段。
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
}

// ParseSliceHeader 解析 slice header 的公共前缀字段。
//
// rbsp 是 slice NAL 的 RBSP（去掉 NAL header 与防竞争字节），可以包含
// 后续 slice data；本函数只消费 header 字段，不校验尾位。
//
// ppsByID 用于按 pic_parameter_set_id 查找 PPS：该字段位于 header 前部，
// 解析器不能依赖调用方预先知道 PPS。idr 由 NAL unit type 是否为 5 决定。
func ParseSliceHeader(rbsp []byte, sps *SPS, ppsByID map[uint32]*PPS, idr bool) (*SliceHeader, error) {
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
	return h, nil
}
