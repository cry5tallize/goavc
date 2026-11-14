package transform

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// MaxScaledQP 是共享8/10-bit量化内核接受的QP'上限：51+QpBdOffset(10)=63。
// 它不是语法QPY上限，也不能用于CABAC上下文初始化。
const MaxScaledQP = 63

// ScaleQP 将语法QPY转换为量化内核使用的QP'=QPY+6*(bitDepth-8)。
// 仅支持8和10位；QPY范围为[-QpBdOffset,51]，越界返回ErrInvalidData。
// 规范允许但尚未实现的位深返回ErrUnsupported，非法位深返回ErrInvalidData。
// 返回值仅用于变换/量化，不得替代CABAC初始化使用的语法QP。
func ScaleQP(qpY, bitDepth int) (int, error) {
	offset, err := qpDepthOffset(bitDepth)
	if err != nil {
		return 0, err
	}
	if qpY < -offset || qpY > MaxQP {
		return 0, fmt.Errorf("transform: syntax QPY %d out of range %d..%d: %w", qpY, -offset, MaxQP, h264.ErrInvalidData)
	}
	return qpY + offset, nil
}

// ScaleChromaQP 将420色度的语法QP转换为QP'C，支持8和10位。
// offset是PPS色度QP偏移[-12,12]，不是位深偏移。先Clip3(-QpBdOffset,51,
// QPY+offset)，再按表8-10映射（负值保持不变），最后加QpBdOffset。
// QPY与位深的边界、错误分类与ScaleQP一致；非法offset返回ErrInvalidData。
func ScaleChromaQP(qpY, offset, bitDepth int) (int, error) {
	qp, err := ScaleQP(qpY, bitDepth)
	if err != nil {
		return 0, err
	}
	if offset < -12 || offset > 12 {
		return 0, fmt.Errorf("transform: chroma QP offset %d out of range -12..12: %w", offset, h264.ErrInvalidData)
	}
	depthOffset := qp - qpY
	qpi := qpY + offset
	if qpi < -depthOffset {
		qpi = -depthOffset
	} else if qpi > MaxQP {
		qpi = MaxQP
	}
	if qpi >= 0 {
		qpi = chromaQPTable[qpi]
	}
	return qpi + depthOffset, nil
}

func qpDepthOffset(bitDepth int) (int, error) {
	switch bitDepth {
	case 8:
		return 0, nil
	case 10:
		return 12, nil
	default:
		if bitDepth < 8 || bitDepth > 14 {
			return 0, fmt.Errorf("transform: invalid bit depth %d: %w", bitDepth, h264.ErrInvalidData)
		}
		return 0, fmt.Errorf("transform: bit depth %d: %w", bitDepth, h264.ErrUnsupported)
	}
}
