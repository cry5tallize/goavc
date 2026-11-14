package syntax

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/transform"
)

// ScalingMatrices 以空间光栅顺序返回有效矩阵。Four 为
// intra Y/Cb/Cr、inter Y/Cb/Cr；Eight 为 intra Y/inter Y、intra Cb/inter Cb、
// intra Cr/inter Cr。非 4:4:4 仅 Eight 的前两项有效，其余项保持零值。
type ScalingMatrices struct {
	Four  [6][16]uint8
	Eight [6][64]uint8
}

var defaultScaling4 = [2][16]uint8{
	{6, 13, 20, 28, 13, 20, 28, 32, 20, 28, 32, 37, 28, 32, 37, 42},
	{10, 14, 20, 24, 14, 20, 24, 27, 20, 24, 27, 30, 24, 27, 30, 34},
}
var defaultScaling8 = [2][64]uint8{
	{6, 10, 13, 16, 18, 23, 25, 27, 10, 11, 16, 18, 23, 25, 27, 29, 13, 16, 18, 23, 25, 27, 29, 31, 16, 18, 23, 25, 27, 29, 31, 33, 18, 23, 25, 27, 29, 31, 33, 36, 23, 25, 27, 29, 31, 33, 36, 38, 25, 27, 29, 31, 33, 36, 38, 40, 27, 29, 31, 33, 36, 38, 40, 42},
	{9, 13, 15, 17, 19, 21, 22, 24, 13, 13, 17, 19, 21, 22, 24, 25, 15, 17, 19, 21, 22, 24, 25, 27, 17, 19, 21, 22, 24, 25, 27, 28, 19, 21, 22, 24, 25, 27, 28, 30, 21, 22, 24, 25, 27, 28, 30, 32, 22, 24, 25, 27, 28, 30, 32, 33, 24, 25, 27, 28, 30, 32, 33, 35},
}

// SelectScalingMatrices 实现 Spec: 7.4.2.1/7.4.2.2 的 fallback A/B。
// 表7-2的12-list索引用于chroma_format_idc=3，即使SeparateColourPlaneFlag
// 为真也保留Cb/Cr列表；各独立平面只替换预测/变换过程，不改变列表选择。
// scaling_list() 的 Values 是扫描顺序；useDefault 由首 delta=-8 表示，
// 此时不能误用解析器为语法往返保留的重复值 8。
func SelectScalingMatrices(s *SPS, p *PPS) (ScalingMatrices, error) {
	var out ScalingMatrices
	if s == nil || p == nil {
		return out, fmt.Errorf("syntax: scaling selection requires SPS/PPS: %w", h264.ErrInvalidData)
	}
	eightCount := 2
	if s.effectiveChromaFormatIDC() == 3 {
		eightCount = 6
	}
	for i := range out.Four {
		for j := range out.Four[i] {
			out.Four[i][j] = 16
		}
	}
	for i := range eightCount {
		for j := range out.Eight[i] {
			out.Eight[i][j] = 16
		}
	}
	fill := func(dst []uint8, list *ScalingList, preset []uint8, scan []int) error {
		if list == nil || (list.Size != 4 && list.Size != 8) || list.Size*list.Size != len(dst) || len(list.Values) != len(dst) {
			return fmt.Errorf("syntax: missing or invalid scaling list dimensions: %w", h264.ErrInvalidData)
		}
		if len(list.Deltas) > 0 && list.Deltas[0] == -8 {
			copy(dst, preset)
			return nil
		}
		for k, v := range list.Values {
			if v < 1 || v > 255 {
				return fmt.Errorf("syntax: scaling value %d: %w", v, h264.ErrInvalidData)
			}
			dst[scan[k]] = uint8(v)
		}
		return nil
	}
	if s.SeqScalingMatrixPresentFlag {
		for i := 0; i < 6; i++ {
			kind := i / 3
			if s.SeqScalingListPresentFlag[i] {
				if err := fill(out.Four[i][:], s.SeqScalingList[i], defaultScaling4[kind][:], transform.Zigzag4x4[:]); err != nil {
					return out, err
				}
			} else if i == 0 || i == 3 {
				out.Four[i] = defaultScaling4[kind]
			} else {
				out.Four[i] = out.Four[i-1]
			}
		}
		for i := range eightCount {
			kind := i % 2
			if s.SeqScalingListPresentFlag[6+i] {
				if err := fill(out.Eight[i][:], s.SeqScalingList[6+i], defaultScaling8[kind][:], transform.Zigzag8x8[:]); err != nil {
					return out, err
				}
			} else if i < 2 {
				out.Eight[i] = defaultScaling8[kind]
			} else {
				out.Eight[i] = out.Eight[i-2]
			}
		}
	}
	if p.PicScalingMatrixPresentFlag {
		for i := 0; i < 6; i++ {
			kind := i / 3
			if p.PicScalingListPresentFlag[i] {
				if err := fill(out.Four[i][:], p.PicScalingList[i], defaultScaling4[kind][:], transform.Zigzag4x4[:]); err != nil {
					return out, err
				}
			} else if i == 0 || i == 3 {
				if !s.SeqScalingMatrixPresentFlag {
					out.Four[i] = defaultScaling4[kind]
				}
			} else {
				out.Four[i] = out.Four[i-1]
			}
		}
		if p.Transform8x8ModeFlag {
			for i := range eightCount {
				kind := i % 2
				if p.PicScalingListPresentFlag[6+i] {
					if err := fill(out.Eight[i][:], p.PicScalingList[6+i], defaultScaling8[kind][:], transform.Zigzag8x8[:]); err != nil {
						return out, err
					}
				} else if i >= 2 {
					// fallback B 的色度列表继承当前 PPS 的同类前一平面，
					// 不能保留 SPS 的对应 Cb/Cr 列表。
					out.Eight[i] = out.Eight[i-2]
				} else if !s.SeqScalingMatrixPresentFlag {
					out.Eight[i] = defaultScaling8[kind]
				}
			}
		}
	}
	return out, nil
}
