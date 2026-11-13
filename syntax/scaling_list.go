package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// ScalingList 是一个 4×4 或 8×8 缩放矩阵的解析结果。
//
// 字段语义：
//   - Size 是边长（4 或 8），系数总数为 Size*Size；
//   - Deltas 是按语法顺序保存的 delta_scale 值，序列化时原样回写，
//     因此 parse→serialize 可以逐字节复现原始码流；
//   - Values 是由 Deltas 按 §7.3.2.1.1.1 推导出的矩阵值，仅用于消费方，
//     序列化不读取它。
type ScalingList struct {
	Size   int
	Deltas []int32
	Values []int32
}

// Count 返回系数个数。
func (s *ScalingList) Count() int {
	if s == nil || s.Size <= 0 {
		return 0
	}
	return s.Size * s.Size
}

// ParseScalingList 解析 scaling_list(sizeOfScalingList)。
//
// size 是矩阵边长（4 或 8），对应规范中的 16 或 64 个系数。
// delta_scale 的合法范围是 -128..127（§7.3.2.1.1.1）；越界值返回
// ErrInvalidData，而不是截断后继续解析。
func ParseScalingList(r *bitstream.Reader, size int) (*ScalingList, error) {
	if size != 4 && size != 8 {
		return nil, fmt.Errorf("syntax: scaling list size %d must be 4 or 8", size)
	}
	count := size * size
	list := &ScalingList{
		Size:   size,
		Deltas: make([]int32, 0, count),
		Values: make([]int32, count),
	}

	lastScale := int32(8)
	nextScale := int32(8)
	for j := 0; j < count; j++ {
		if nextScale != 0 {
			delta, err := r.ReadSE()
			if err != nil {
				return nil, fmt.Errorf("syntax: scaling_list delta[%d]: %w", j, err)
			}
			if delta < -128 || delta > 127 {
				return nil, fmt.Errorf("syntax: scaling_list delta[%d] = %d out of range -128..127: %w",
					j, delta, h264.ErrInvalidData)
			}
			list.Deltas = append(list.Deltas, delta)
			nextScale = (lastScale + delta + 256) % 256
		}
		value := lastScale
		if nextScale != 0 {
			value = nextScale
		}
		list.Values[j] = value
		lastScale = value
	}
	return list, nil
}

// WriteTo 按保存的 Deltas 重新写出 scaling_list。
//
// 写出前会重放一遍推导过程，确保 Deltas 的数量与 Size 匹配；数量过多
// 或不足都返回错误，不会输出半个矩阵。
func (s *ScalingList) WriteTo(w *bitstream.Writer) error {
	if s == nil {
		return fmt.Errorf("syntax: nil scaling list")
	}
	if s.Size != 4 && s.Size != 8 {
		return fmt.Errorf("syntax: scaling list size %d must be 4 or 8", s.Size)
	}
	count := s.Size * s.Size
	lastScale := int32(8)
	nextScale := int32(8)
	di := 0
	for j := 0; j < count; j++ {
		if nextScale != 0 {
			if di >= len(s.Deltas) {
				return fmt.Errorf("syntax: scaling list needs more than %d deltas", len(s.Deltas))
			}
			delta := s.Deltas[di]
			di++
			if delta < -128 || delta > 127 {
				return fmt.Errorf("syntax: scaling list delta[%d] = %d out of range", j, delta)
			}
			if err := w.WriteSE(delta); err != nil {
				return fmt.Errorf("syntax: write scaling_list delta[%d]: %w", j, err)
			}
			nextScale = (lastScale + delta + 256) % 256
		}
		value := lastScale
		if nextScale != 0 {
			value = nextScale
		}
		lastScale = value
	}
	if di != len(s.Deltas) {
		return fmt.Errorf("syntax: scaling list has %d unused deltas", len(s.Deltas)-di)
	}
	return nil
}
