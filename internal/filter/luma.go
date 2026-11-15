package filter

import (
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/simd"
)

// LumaEdge 对一条亮度边界的 4 像素线段做去块滤波（8.7.2.2/8.7.2.3）。
//
// 参数：
//   - data 是包含 p 侧与 q 侧的完整像素缓冲；
//   - q0 是 q0（边界右侧/下侧第一个像素）在 data 中的下标；
//   - lineStep 是相邻两条线之间的下标差（垂直边界用 stride，水平边界用 1）；
//   - acrossStep 是从 q0 到 p0 的下标差（垂直边界用 1，水平边界用 stride）。
//
// bS 为 0 时不做任何修改；1..3 使用弱滤波，4 使用强滤波。
// qp、alphaDiv2、betaDiv2 由调用方按宏块/切片给出；索引计算遵循
// FFmpeg 的 qp + 52 + 2*div2 约定。
func LumaEdge(data []byte, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2 int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, 4, 8)
}

// LumaEdgeHigh 接收去掉 QpBdOffset 的语法域 QP；所有步长以采样计。
func LumaEdgeHigh(data []uint16, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, bitDepth int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, 4, bitDepth)
}

// LumaEdgeSpan 供MBAFF混合边界按实际帧/场样点分段，仍复用同一滤波算术。
func LumaEdgeSpan(data []byte, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, 8)
}

// LumaEdgeHighSpan 的步长和跨度以原生高位深采样计。
func LumaEdgeHighSpan(data []uint16, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth)
}

func lumaEdge[T sample](data []T, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth int) {
	if bS <= 0 {
		return
	}
	indexA := qp + 52 + 2*alphaDiv2
	indexB := qp + 52 + 2*betaDiv2
	if indexA < 0 || indexA >= len(alphaTable) || indexB < 0 || indexB >= len(betaTable) {
		return
	}
	scale := 1 << (bitDepth - 8)
	alpha, beta := int(alphaTable[indexA])*scale, int(betaTable[indexB])*scale
	tc := tc0(bS, indexA) * scale
	switch d := any(data).(type) {
	case []byte:
		simd.FilterLuma8(d, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth)
	case []uint16:
		simd.FilterLuma16(d, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth)
	}
}

func tc0(bS, indexA int) int {
	if indexA < 0 || indexA >= len(tc0Table) || bS < 0 || bS > 3 {
		return -1
	}
	return int(tc0Table[indexA][bS])
}

type sample interface {
	~uint8 | ~uint16
}

// ErrInvalidEdge 用于调用方报告越界的边界参数（保留给后续 slice 并行
// 实现做防御性检查；正常路径不会触发）。
var ErrInvalidEdge = h264.ErrInvalidData
