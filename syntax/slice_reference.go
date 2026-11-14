package syntax

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// RefListModification 保存表 7-7 的重排命令；Value 对应 abs_diff_pic_num_minus1
// 或 long_term_pic_num。保留原始命令，不能只消费位流后丢弃参考身份。
type RefListModification struct{ IDC, Value uint32 }

// MMCO 保存 Spec: 7.3.3.3 中的参考图像标记操作，字段按各操作码解释。
type MMCO struct {
	Operation                 uint32
	DifferenceOfPicNumsMinus1 uint32
	LongTermPicNum            uint32
	LongTermFrameIdx          uint32
	MaxLongTermFrameIdxPlus1  uint32
}

type PredictionWeight struct{ Weight, Offset [3]int32 }
type WeightTable struct {
	LumaDenom, ChromaDenom uint32
	Lists                  [2][]PredictionWeight
}

func parseRefPicListReordering(r *bitstream.Reader) ([]RefListModification, error) {
	var out []RefListModification
	for i := 0; i < 66; i++ {
		idc, err := readUE(r, "reordering_idc")
		if err != nil {
			return nil, err
		}
		if idc == 3 {
			return out, nil
		}
		if idc > 3 {
			return nil, fmt.Errorf("syntax: reordering idc %d: %w", idc, h264.ErrInvalidData)
		}
		v, err := readUE(r, "reordering_value")
		if err != nil {
			return nil, err
		}
		out = append(out, RefListModification{idc, v})
	}
	return nil, fmt.Errorf("syntax: too many reference reordering commands: %w", h264.ErrInvalidData)
}

func parsePredWeightTable(r *bitstream.Reader, sps *SPS, typ SliceType, l0, l1 uint32) (*WeightTable, error) {
	t := &WeightTable{}
	var err error
	t.LumaDenom, err = readUE(r, "luma_log2_weight_denom")
	if err != nil {
		return nil, err
	}
	if !sps.SeparateColourPlaneFlag && sps.effectiveChromaFormatIDC() != 0 {
		t.ChromaDenom, err = readUE(r, "chroma_log2_weight_denom")
		if err != nil {
			return nil, err
		}
	}
	if t.LumaDenom > 7 || t.ChromaDenom > 7 {
		return nil, fmt.Errorf("syntax: weight denominator exceeds 7: %w", h264.ErrInvalidData)
	}
	readPair := func(name string) (int32, int32, error) {
		w, e := readSE(r, name+"_weight")
		if e != nil {
			return 0, 0, e
		}
		o, e := readSE(r, name+"_offset")
		if e != nil {
			return 0, 0, e
		}
		if w < -128 || w > 127 || o < -128 || o > 127 {
			return 0, 0, fmt.Errorf("syntax: weight or offset outside -128..127: %w", h264.ErrInvalidData)
		}
		return w, o, nil
	}
	lists := 1
	if typ == SliceTypeB {
		lists = 2
	}
	for list := 0; list < lists; list++ {
		n := l0 + 1
		if list == 1 {
			n = l1 + 1
		}
		if n > 32 {
			return nil, fmt.Errorf("syntax: weight reference count exceeds 32: %w", h264.ErrInvalidData)
		}
		t.Lists[list] = make([]PredictionWeight, n)
		for i := range t.Lists[list] {
			p := &t.Lists[list][i]
			p.Weight = [3]int32{1 << t.LumaDenom, 1 << t.ChromaDenom, 1 << t.ChromaDenom}
			flag, e := readFlag(r, "luma_weight_flag")
			if e != nil {
				return nil, e
			}
			if flag {
				p.Weight[0], p.Offset[0], err = readPair("luma")
				if err != nil {
					return nil, err
				}
			}
			if !sps.SeparateColourPlaneFlag && sps.effectiveChromaFormatIDC() != 0 {
				flag, e = readFlag(r, "chroma_weight_flag")
				if e != nil {
					return nil, e
				}
				if flag {
					for c := 1; c < 3; c++ {
						p.Weight[c], p.Offset[c], err = readPair("chroma")
						if err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	return t, nil
}

func parseDecRefPicMarking(r *bitstream.Reader) ([]MMCO, error) {
	var out []MMCO
	for count := 0; count < 66; count++ {
		op, err := readUE(r, "memory_management_control_operation")
		if err != nil {
			return nil, err
		}
		if op == 0 {
			return out, nil
		}
		m := MMCO{Operation: op}
		if op == 1 || op == 3 {
			m.DifferenceOfPicNumsMinus1, err = readUE(r, "difference_of_pic_nums_minus1")
			if err != nil {
				return nil, err
			}
		}
		switch op {
		case 1:
		case 2:
			m.LongTermPicNum, err = readUE(r, "long_term_pic_num")
		case 3, 6:
			m.LongTermFrameIdx, err = readUE(r, "long_term_frame_idx")
		case 4:
			m.MaxLongTermFrameIdxPlus1, err = readUE(r, "max_long_term_frame_idx_plus1")
		case 5:
		default:
			return nil, fmt.Errorf("syntax: MMCO operation %d exceeds 6: %w", op, h264.ErrInvalidData)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return nil, fmt.Errorf("syntax: too many MMCO commands: %w", h264.ErrInvalidData)
}
