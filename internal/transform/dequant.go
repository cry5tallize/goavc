package transform

// levelScale4x4 是规范表 8-15 的 levelScale4x4[qP%6][i][j]（光栅顺序）。
//
// 表值按 i、j 的奇偶性取三个数 a、b、c：
// (偶,偶)=a、(奇,偶)/(偶,奇)=b、(奇,奇)=c。qP%6=0 时为 10/13/16，
// 其余行为 11/14/18、13/16/20、14/18/23、16/20/25、18/23/29。
//
// 历史上这里曾把 (1,1) 写成 11，导致所有残差偏小约 5%～40%，
// 而语法解析仍然完全正常；只有逐像素 Oracle 才能发现。
var levelScale4x4 = [6][16]int32{
	{10, 13, 10, 13, 13, 16, 13, 16, 10, 13, 10, 13, 13, 16, 13, 16},
	{11, 14, 11, 14, 14, 18, 14, 18, 11, 14, 11, 14, 14, 18, 14, 18},
	{13, 16, 13, 16, 16, 20, 16, 20, 13, 16, 13, 16, 16, 20, 16, 20},
	{14, 18, 14, 18, 18, 23, 18, 23, 14, 18, 14, 18, 18, 23, 18, 23},
	{16, 20, 16, 20, 20, 25, 20, 25, 16, 20, 16, 20, 20, 25, 20, 25},
	{18, 23, 18, 23, 23, 29, 23, 29, 18, 23, 18, 23, 23, 29, 23, 29},
}

// MaxQP 是8-bit语法QPY上限；10-bit语法上限仍为51，内核QP'上限见MaxScaledQP。
const MaxQP = 51

// dequantMul 返回 FFmpeg/libavcodec 使用的定点乘子。
//
// 推导（与规范 8.5.12.1 等价，但把右移合并到乘子里以便统一处理 qP 高低段）：
//
//	qmul = levelScale * 16 << (qP/6 + 2)
//	d    = (level*qmul + 32) >> 6
//
// 其中的 16 是默认 scaling matrix 的归一化因子；FFmpeg 的 dequant4_coeff
// 把 scaling_matrix4（默认全 16）直接乘进 qmul。漏掉这个因子会让所有
// 残差小 16 倍，而语法解析仍然“正常”，是逐像素对比才能发现的错误。
//
// 当 qP >= 24 时 qmul 至少是 levelScale<<6，上式是精确的左移；
// 当 qP < 24 时 +32>>6 恰好复现规范中的 +2^(3-qP/6) 舍入。
func dequantMul(scale int32, qp int, matrix uint8) int64 {
	return int64(scale) * int64(matrix) << (uint(qp)/6 + 2)
}

// Dequant4x4 对 16 个系数做 4×4 反量化。
//
// levels 与 dst 都按光栅顺序（row*4+col），可以指向同一数组。
// qp 是已转换的有效QP'，必须在0..MaxScaledQP内；越界返回ErrInvalidData。
// 系数乘积用 int64 计算并检查 int32 溢出，防止恶意 level 造成回绕。
func Dequant4x4(dst *[16]int32, levels *[16]int32, qp int) error {
	return Dequant4x4WithScaling(dst, levels, qp, nil)
}

// ChromaQP 按表 8-10 映射 Clip3(0,51,QPY+offset)，包括非线性高 QP 段。
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
