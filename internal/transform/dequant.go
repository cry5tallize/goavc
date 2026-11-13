package transform

import (
	"fmt"

	h264 "github.com/goavc/h264"
)

// levelScale4x4 是规范表 8-15 的 levelScale4x4[qP%6][i][j]（光栅顺序）。
//
// 表值按 i、j 的奇偶性分布：
//
//	qP%6=0: 10 13 10 13 / 13 11 13 11 / ...
//	qP%6=1: 16 18 16 18 / 18 14 18 14 / ...
//
// 每一行按 row*4+col 展开。使用显式常量表而不是运行时公式，是为了让
// 与规范表格的逐项对照测试可以直接覆盖。
var levelScale4x4 = [6][16]int32{
	{10, 13, 10, 13, 13, 11, 13, 11, 10, 13, 10, 13, 13, 11, 13, 11},
	{16, 18, 16, 18, 18, 14, 18, 14, 16, 18, 16, 18, 18, 14, 18, 14},
	{13, 14, 13, 14, 14, 10, 14, 10, 13, 14, 13, 14, 14, 10, 14, 10},
	{11, 13, 11, 13, 13, 9, 13, 9, 11, 13, 11, 13, 13, 9, 13, 9},
	{18, 21, 18, 21, 21, 16, 21, 16, 18, 21, 18, 21, 21, 16, 21, 16},
	{14, 16, 14, 16, 16, 12, 16, 12, 14, 16, 14, 16, 16, 12, 16, 12},
}

// MaxQP 是 8-bit 4:2:0 的 QP 上限。
const MaxQP = 51

// dequantMul 返回 FFmpeg/libavcodec 使用的定点乘子。
//
// 推导（与规范 8.5.12.1 等价，但把右移合并到乘子里以便统一处理 qP 高低段）：
//
//	qmul = levelScale << (qP/6 + 2)
//	d    = (level*qmul + 32) >> 6
//
// 当 qP >= 24 时 qmul 至少是 levelScale<<6，上式是精确的左移；
// 当 qP < 24 时 +32>>6 恰好复现规范中的 +2^(3-qP/6) 舍入。
// 这个形式与 FFmpeg 的 dequant4_coeff 完全一致，是逐像素 Oracle 对比的前提。
func dequantMul(scale int32, qp int) int64 {
	return int64(scale) << (uint(qp)/6 + 2)
}

// Dequant4x4 对 16 个系数做 4×4 反量化。
//
// levels 与 dst 都按光栅顺序（row*4+col），可以指向同一数组。
// qp 必须在 0..51 内；越界返回 ErrInvalidData 而不是静默截断。
// 系数乘积用 int64 计算并检查 int32 溢出，防止恶意 level 造成回绕。
func Dequant4x4(dst *[16]int32, levels *[16]int32, qp int) error {
	if qp < 0 || qp > MaxQP {
		return fmt.Errorf("transform: qp %d out of range 0..%d: %w", qp, MaxQP, h264.ErrInvalidData)
	}
	row := &levelScale4x4[qp%6]
	for i := 0; i < 16; i++ {
		v := (int64(levels[i])*dequantMul(row[i], qp) + 32) >> 6
		if v < -1<<31 || v > 1<<31-1 {
			return fmt.Errorf("transform: dequantized coefficient %d overflows int32: %w",
				v, h264.ErrInvalidData)
		}
		dst[i] = int32(v)
	}
	return nil
}

// DequantLumaDC 把 16 个亮度 DC level 反量化并做 4×4 逆 Hadamard。
// 表在 30 之后压缩，直接使用常量表可以避免手工分段写错。
func ChromaQP(qpY, offset int) int {
	qpi := qpY + offset
	if qpi < 0 {
		qpi = 0
	} else if qpi > MaxQP {
		qpi = MaxQP
	}
	return chromaQPTable[qpi]
}

// chromaQPTable 是表 8-10 在 8-bit 下的 52 项映射。
var chromaQPTable = [52]int{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
	10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
	20, 21, 22, 23, 24, 25, 26, 27, 28, 29,
	29, 30, 31, 32, 32, 33, 34, 34, 35, 35,
	36, 36, 37, 37, 37, 38, 38, 38, 39, 39,
	39, 39,
}
