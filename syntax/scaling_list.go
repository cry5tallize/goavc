package syntax

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// ScalingList 是一个 4×4 或 8×8 缩放矩阵的解析结果。
//
// 字段语义：
//   - Size 是边长（4 或 8），系数总数为 Size*Size；
//   - Deltas 是按语法顺序保存的 delta_scale 值，序列化时原样回写，
//     因此 parse→serialize 可以逐字节复现原始码流；
//   - Values 是由 Deltas 按 §7.3.2.1.1.1 推导出的消费方缓存；
//     写出时验证其与 Deltas 一致，但仍按原 Deltas 保留语法表示。
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
		return nil, fmt.Errorf("syntax: scaling list size %d must be 4 or 8: %w", size, h264.ErrInvalidData)
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
// 写出前完整重放 Deltas；非法列表不会改变 writer。若带 Values 缓存，
// 非 useDefault 列表必须与语法一致，避免本地消费者和回读码流使用不同矩阵。
func (s *ScalingList) WriteTo(w *bitstream.Writer) error {
	if err := s.validateSyntax(); err != nil {
		return err
	}
	for j, delta := range s.Deltas {
		if err := w.WriteSE(delta); err != nil {
			return fmt.Errorf("syntax: write scaling_list delta[%d]: %w", j, err)
		}
	}
	return nil
}

// validateSyntax 不分配临时码流，允许只有 Deltas 的写入用列表。
// Values-only 是选择器的已有内存接口，但不能冒充可序列化语法。
func (s *ScalingList) validateSyntax() error {
	if s == nil || (s.Size != 4 && s.Size != 8) {
		return fmt.Errorf("syntax: missing or invalid scaling list size: %w", h264.ErrInvalidData)
	}
	count := s.Size * s.Size
	if s.Values != nil && len(s.Values) != count {
		return fmt.Errorf("syntax: scaling list has %d cached values, want %d: %w", len(s.Values), count, h264.ErrInvalidData)
	}
	lastScale, nextScale := int32(8), int32(8)
	di := 0
	useDefault := len(s.Deltas) > 0 && s.Deltas[0] == -8
	for j := range count {
		if nextScale != 0 {
			if di >= len(s.Deltas) {
				return fmt.Errorf("syntax: scaling list needs more than %d deltas: %w", len(s.Deltas), h264.ErrInvalidData)
			}
			delta := s.Deltas[di]
			di++
			if delta < -128 || delta > 127 {
				return fmt.Errorf("syntax: scaling list delta[%d] = %d out of range: %w", j, delta, h264.ErrInvalidData)
			}
			nextScale = (lastScale + delta + 256) % 256
		}
		value := lastScale
		if nextScale != 0 {
			value = nextScale
		}
		if !useDefault && s.Values != nil && s.Values[j] != value {
			return fmt.Errorf("syntax: scaling list cached value[%d] = %d differs from syntax value %d: %w", j, s.Values[j], value, h264.ErrInvalidData)
		}
		lastScale = value
	}
	if di != len(s.Deltas) {
		return fmt.Errorf("syntax: scaling list has %d unused deltas: %w", len(s.Deltas)-di, h264.ErrInvalidData)
	}
	return nil
}
