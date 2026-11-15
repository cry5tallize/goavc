package transform

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/simd"
)

// DC2x4Scan 是 4:2:2 色度 DC 的扫描序号到光栅下标（row*2+col）的映射。
// Spec: 8.5.4（2010），输入列表在空间中的序号为 {0,2;1,5;3,6;4,7}。
// 它不随帧/场扫描选择而改变；不得截取 Zigzag4x4 的前八项。
var DC2x4Scan = [8]int{0, 2, 1, 4, 6, 3, 5, 7}

// ForwardChromaDC422 变换八个色度 4×4 子块的 DC，输入输出均为 2×4 光栅顺序。
// 使用 H4*src*H2，不做额外归一化（Hadamard 总增益为 8）。
// 随后复用 QuantDCWithScaling/QuantInterDCWithScaling，传入 qpC+3 和矩阵首项；
// 量化器自身的额外一位吸收 DC 缩放，不可再把本函数输出除二。
// qpC 是 ScaleChromaQP 得到的 QP'C，而非语法 QPC，也不能先对 QPY 加三再映射。
// 8/10-bit 的合法 QP'C 最大为 39/51，因此 qpC+3 不超出既有 QuantDC 的上限。
// 变换旁路不调用本函数及 DC 量化；直接保留各子块 DC。
// int64 中间量和整块提交保证正负溢出被拒绝，且允许 dst==src。
func ForwardChromaDC422(dst, src *[8]int32) error {
	if dst == nil || src == nil {
		return fmt.Errorf("transform: nil 422 DC block: %w", h264.ErrInvalidData)
	}
	values := simd.ForwardChromaDC422(*src)
	var out [8]int32
	for i, v := range values {
		if v < -1<<31 || v > 1<<31-1 {
			return fmt.Errorf("transform: forward 422 DC coefficient overflows int32: %w", h264.ErrInvalidData)
		}
		out[i] = int32(v)
	}
	*dst = out
	return nil
}

// DequantChromaDC422 是平坦缩放矩阵的八 DC 共享重建入口。
func DequantChromaDC422(dst, levels *[8]int32, qpC int) error {
	return DequantChromaDC422WithScaling(dst, levels, qpC, 16)
}

// DequantChromaDC422WithScaling 做规范 8.5.11 的 2×4 逆 Hadamard 与缩放
// （2007 版为 8.5.10）。qpC 是包含位深偏移的 QP'C，scale 为 4×4 矩阵首项。
// 仅 ChromaArrayType=2 且非变换旁路时使用 qPDC=qpC+3；偏移在这里加一次。
// 令 f=H4*levels*H2，则 d=(f*LevelScale4x4[qPDC%6][0]*scale*2^(qPDC/6)+32)>>6。
// 高 QP 段等价于规范的精确左移；低段必须有舍入，不能套用 420 的无舍入 >>5。
// 输出作为八个子块反量化后系数[0]，仍须经过共享 4×4 逆变换的 >>6 归一化。
// 变换旁路不得调用本函数；非法 QP、零矩阵或 int32 溢出均不修改 dst。
func DequantChromaDC422WithScaling(dst, levels *[8]int32, qpC int, scale uint8) error {
	if dst == nil || levels == nil || qpC < 0 || qpC > MaxScaledQP || scale == 0 {
		return fmt.Errorf("transform: invalid 422 DC scaling: %w", h264.ErrInvalidData)
	}
	qpDC := qpC + 3
	mul := int64(levelScale4x4[qpDC%6][0]) * int64(scale) << uint(qpDC/6)
	out, written := simd.DequantChromaDC422(*levels, mul)
	if written != len(out) {
		return fmt.Errorf("transform: inverse 422 DC coefficient overflows int32: %w", h264.ErrInvalidData)
	}
	*dst = out
	return nil
}
