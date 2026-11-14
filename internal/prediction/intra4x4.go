// Package prediction 实现编解码共享的帧内/帧间预测。
//
// 编码器与解码器必须调用同一份实现（docs/architecture.md §3）。本包只做
// 像素预测，不访问宏块语法；边界可用性与模式回退由 decoder 层决定。
package prediction

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// 帧内 4×4 模式编号（规范表 8-2）。
const (
	Intra4x4Vertical       = 0
	Intra4x4Horizontal     = 1
	Intra4x4DC             = 2
	Intra4x4DiagonalLeft   = 3
	Intra4x4DiagonalRight  = 4
	Intra4x4VerticalRight  = 5
	Intra4x4HorizontalDown = 6
	Intra4x4VerticalLeft   = 7
	Intra4x4HorizontalUp   = 8
)

// Intra4x4Samples 是 4×4 预测所需的邻域样本。
//
// Top 是上方 A..D，TopRight 是右上方 E..H，Left 是左侧 I..L，
// TopLeft 是左上 M。调用方负责在右上不可用时用 Top[3] 复制填充，
// 这是规范 8.3.1.2 的边界外推规则。
type Intra4x4Samples = intra4x4Samples[byte]

// Intra4x4Samples16 保留完整高位深邻域，不进行降位深转换。
type Intra4x4Samples16 = intra4x4Samples[uint16]

type intra4x4Samples[T sample] struct {
	Top      [4]T
	TopRight [4]T
	Left     [4]T
	TopLeft  T
}

// Intra4x4 计算一个 4×4 帧内预测块并写入 dst（stride 为字节跨度）。
//
// topAvail/leftAvail 只影响 DC 模式；其他模式要求对应方向可用，
// 不可用时返回 ErrInvalidData，由调用方按规范回退到 DC 模式。
func Intra4x4(dst []byte, stride, mode int, s Intra4x4Samples, topAvail, leftAvail bool) error {
	return intra4x4(dst, stride, mode, s, topAvail, leftAvail, 8)
}

// Intra4x4High 的 stride 以采样计，缺少上下文时的 DC 为 1<<(bitDepth-1)。
func Intra4x4High(dst []uint16, stride, mode int, s Intra4x4Samples16, topAvail, leftAvail bool, bitDepth int) error {
	if err := checkBitDepth(bitDepth); err != nil {
		return err
	}
	return intra4x4(dst, stride, mode, s, topAvail, leftAvail, bitDepth)
}

func intra4x4[T sample](dst []T, stride, mode int, s intra4x4Samples[T], topAvail, leftAvail bool, bitDepth int) error {
	if len(dst) < 3*stride+4 || stride < 4 {
		return fmt.Errorf("prediction: intra4x4 destination too small: %w", h264.ErrInvalidData)
	}
	a, b, c, d := s.Top[0], s.Top[1], s.Top[2], s.Top[3]
	e, f, g, h := s.TopRight[0], s.TopRight[1], s.TopRight[2], s.TopRight[3]
	i0, j, k, l := s.Left[0], s.Left[1], s.Left[2], s.Left[3]
	m := s.TopLeft

	switch mode {
	case Intra4x4Vertical:
		if !topAvail {
			return fmt.Errorf("prediction: vertical mode without top samples: %w", h264.ErrInvalidData)
		}
		for y := 0; y < 4; y++ {
			row := dst[y*stride : y*stride+4]
			row[0], row[1], row[2], row[3] = a, b, c, d
		}
	case Intra4x4Horizontal:
		if !leftAvail {
			return fmt.Errorf("prediction: horizontal mode without left samples: %w", h264.ErrInvalidData)
		}
		vals := [4]T{i0, j, k, l}
		for y := 0; y < 4; y++ {
			row := dst[y*stride : y*stride+4]
			row[0], row[1], row[2], row[3] = vals[y], vals[y], vals[y], vals[y]
		}
	case Intra4x4DC:
		dc := intraDC4(s, topAvail, leftAvail, bitDepth)
		for y := 0; y < 4; y++ {
			row := dst[y*stride : y*stride+4]
			row[0], row[1], row[2], row[3] = dc, dc, dc, dc
		}
	case Intra4x4DiagonalLeft:
		if !topAvail {
			return fmt.Errorf("prediction: diagonal down-left without top samples: %w", h264.ErrInvalidData)
		}
		p := [16]T{
			avg3(a, b, c), avg3(b, c, d), avg3(c, d, e), avg3(d, e, f),
			avg3(b, c, d), avg3(c, d, e), avg3(d, e, f), avg3(e, f, g),
			avg3(c, d, e), avg3(d, e, f), avg3(e, f, g), avg3(f, g, h),
			avg3(d, e, f), avg3(e, f, g), avg3(f, g, h), avg3(g, h, h),
		}
		write4x4(dst, stride, p)
	case Intra4x4DiagonalRight:
		if !topAvail || !leftAvail {
			return fmt.Errorf("prediction: diagonal down-right requires top and left: %w", h264.ErrInvalidData)
		}
		// 与 libavcodec 的 pred4x4_down_right 逐行对应：
		// 对角线以上用左上/上侧样本，以下用左侧样本。
		p := [16]T{
			avg3(i0, m, a), avg3(m, a, b), avg3(a, b, c), avg3(b, c, d),
			avg3(j, i0, m), avg3(i0, m, a), avg3(m, a, b), avg3(a, b, c),
			avg3(k, j, i0), avg3(j, i0, m), avg3(i0, m, a), avg3(m, a, b),
			avg3(l, k, j), avg3(k, j, i0), avg3(j, i0, m), avg3(i0, m, a),
		}
		write4x4(dst, stride, p)
	case Intra4x4VerticalRight:
		if !topAvail || !leftAvail {
			return fmt.Errorf("prediction: vertical-right requires top and left: %w", h264.ErrInvalidData)
		}
		// 与 pred4x4_vertical_right 对应：上半部分沿垂直方向，
		// 下半部分转向左侧样本。
		p := [16]T{
			avg2(m, a), avg2(a, b), avg2(b, c), avg2(c, d),
			avg3(i0, m, a), avg3(m, a, b), avg3(a, b, c), avg3(b, c, d),
			avg3(m, i0, j), avg2(m, a), avg2(a, b), avg2(b, c),
			avg3(i0, j, k), avg3(i0, m, a), avg3(m, a, b), avg3(a, b, c),
		}
		write4x4(dst, stride, p)
	case Intra4x4HorizontalDown:
		if !topAvail || !leftAvail {
			return fmt.Errorf("prediction: horizontal-down requires top and left: %w", h264.ErrInvalidData)
		}
		// 与 pred4x4_horizontal_down 对应：左上三角从左侧样本过渡到
		// 上方样本，右下半部分重复左侧插值。
		p := [16]T{
			avg2(m, i0), avg3(i0, m, a), avg3(m, a, b), avg3(a, b, c),
			avg2(i0, j), avg3(m, i0, j), avg2(m, i0), avg3(i0, m, a),
			avg2(j, k), avg3(i0, j, k), avg2(i0, j), avg3(m, i0, j),
			avg2(k, l), avg3(j, k, l), avg2(j, k), avg3(i0, j, k),
		}
		write4x4(dst, stride, p)
	case Intra4x4VerticalLeft:
		if !topAvail {
			return fmt.Errorf("prediction: vertical-left without top samples: %w", h264.ErrInvalidData)
		}
		// 与 pred4x4_vertical_left 对应：奇数列取半像素，偶数列取
		// 三抽头；两者交替，不是简单的 2×2 重复。
		p := [16]T{
			avg2(a, b), avg2(b, c), avg2(c, d), avg2(d, e),
			avg3(a, b, c), avg3(b, c, d), avg3(c, d, e), avg3(d, e, f),
			avg2(b, c), avg2(c, d), avg2(d, e), avg2(e, f),
			avg3(b, c, d), avg3(c, d, e), avg3(d, e, f), avg3(e, f, g),
		}
		write4x4(dst, stride, p)
	case Intra4x4HorizontalUp:
		if !leftAvail {
			return fmt.Errorf("prediction: horizontal-up without left samples: %w", h264.ErrInvalidData)
		}
		p := [16]T{
			avg2(i0, j), avg3(i0, j, k), avg2(j, k), avg3(j, k, l),
			avg2(j, k), avg3(j, k, l), avg2(k, l), avg3(k, l, l),
			avg2(k, l), avg3(k, l, l), l, l,
			l, l, l, l,
		}
		write4x4(dst, stride, p)
	default:
		return fmt.Errorf("prediction: invalid intra4x4 mode %d: %w", mode, h264.ErrInvalidData)
	}
	return nil
}

func intraDC4[T sample](s intra4x4Samples[T], topAvail, leftAvail bool, bitDepth int) T {
	switch {
	case topAvail && leftAvail:
		sum := int(s.Top[0]) + int(s.Top[1]) + int(s.Top[2]) + int(s.Top[3]) +
			int(s.Left[0]) + int(s.Left[1]) + int(s.Left[2]) + int(s.Left[3])
		return T((sum + 4) >> 3)
	case topAvail:
		sum := int(s.Top[0]) + int(s.Top[1]) + int(s.Top[2]) + int(s.Top[3])
		return T((sum + 2) >> 2)
	case leftAvail:
		sum := int(s.Left[0]) + int(s.Left[1]) + int(s.Left[2]) + int(s.Left[3])
		return T((sum + 2) >> 2)
	default:
		return T(1 << (bitDepth - 1))
	}
}

func write4x4[T sample](dst []T, stride int, p [16]T) {
	copy(dst[0*stride:0*stride+4], p[0:4])
	copy(dst[1*stride:1*stride+4], p[4:8])
	copy(dst[2*stride:2*stride+4], p[8:12])
	copy(dst[3*stride:3*stride+4], p[12:16])
}

func avg2[T sample](a, b T) T { return T((int(a) + int(b) + 1) >> 1) }
func avg3[T sample](a, b, c T) T {
	return T((int(a) + 2*int(b) + int(c) + 2) >> 2)
}
