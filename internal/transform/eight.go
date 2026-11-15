package transform

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/simd"
)

// Zigzag8x8 是 Spec: 8.5 的逐帧 8×8 光栅反扫描，供两种熵模式共用。
var Zigzag8x8 = [64]int{
	0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
}

var levelScale8 = [6][6]int64{{20, 18, 32, 19, 25, 24}, {22, 19, 35, 21, 28, 26}, {26, 23, 42, 24, 33, 31}, {28, 25, 45, 26, 35, 33}, {32, 28, 51, 30, 40, 38}, {36, 32, 58, 34, 46, 43}}
var scale8Class = [16]int{0, 3, 4, 3, 3, 1, 5, 1, 4, 5, 2, 5, 3, 1, 5, 1}


// Forward8x8 将[-1023,1023]的8/10-bit残差投影到规范逆变换的正交基。
// 输入输出按光栅顺序，可原地调用；不使用浮点近似或另一份逆变换。
// IDCT输出为 A*C*Aᵀ/64，故 C=64*Aᵀ*src*A/(各轴平方范数)。
// 基底放大64后分子系数为2^18；合法残差的全部中间量在int64范围内。
func Forward8x8(dst, src *[64]int32) {
	simd.Forward8x8(dst, src)
}


// Quant8x8WithScaling 以解码器的逆量化矩阵为基准选择量化级。
// 量化器不是码流规范的一部分；反推实际 inverse scale 保证矩阵应用和
// QP 缩放与 Dequant8x8 完全配对。nil scaling 表示 4:2:0 flat=16。
// qp是已转换的QP'，范围0..MaxScaledQP；帧内和帧间共用该边界。
func Quant8x8WithScaling(dst, src *[64]int32, qp int, scaling *[64]uint8) error {
	return quant8x8WithScaling(dst, src, qp, scaling, 3)
}

// QuantInter8x8WithScaling 与4×4帧间路径采用相同的1/6死区。
func QuantInter8x8WithScaling(dst, src *[64]int32, qp int, scaling *[64]uint8) error {
	return quant8x8WithScaling(dst, src, qp, scaling, 6)
}

func quant8x8WithScaling(dst, src *[64]int32, qp int, scaling *[64]uint8, divisor int64) error {
	if qp < 0 || qp > MaxScaledQP {
		return fmt.Errorf("transform: 8x8 quant QP %d: %w", qp, h264.ErrInvalidData)
	}
	for i, value := range src {
		matrix := uint8(16)
		if scaling != nil {
			matrix = scaling[i]
		}
		if matrix == 0 {
			return fmt.Errorf("transform: zero 8x8 quant scaling entry: %w", h264.ErrInvalidData)
		}
		class := scale8Class[(i/8%4)*4+i%4]
		mul := levelScale8[qp%6][class] * int64(matrix) << uint(qp/6)
		v := int64(value)
		if v < 0 {
			v = -v
		}
		level := (v*64 + mul/divisor) / mul
		if src[i] < 0 {
			level = -level
		}
		if level < -1<<31 || level > 1<<31-1 {
			return fmt.Errorf("transform: 8x8 quantized level overflow: %w", h264.ErrInvalidData)
		}
		dst[i] = int32(level)
	}
	return nil
}

// Dequant8x8 接受已转换的QP'（0..MaxScaledQP），输出检查int32溢出。
func Dequant8x8(dst, levels *[64]int32, qp int, scaling *[64]uint8) error {
	if qp < 0 || qp > MaxScaledQP {
		return fmt.Errorf("transform: 8x8 QP %d: %w", qp, h264.ErrInvalidData)
	}
	for i := 0; i < 64; i++ {
		m := uint8(16)
		if scaling != nil {
			m = scaling[i]
		}
		if m == 0 {
			return fmt.Errorf("transform: zero 8x8 scaling entry: %w", h264.ErrInvalidData)
		}
		class := scale8Class[(i/8%4)*4+i%4]
		mul := levelScale8[qp%6][class] * int64(m) << uint(qp/6)
		v := (int64(levels[i])*mul + 32) >> 6
		if v < -1<<31 || v > 1<<31-1 {
			return fmt.Errorf("transform: 8x8 coefficient overflow: %w", h264.ErrInvalidData)
		}
		dst[i] = int32(v)
	}
	return nil
}

// IDCT8x8 按光栅布局先水平、再垂直变换；临时量使用 int64。
// Spec: 8.5.13。共享内核负责算术和溢出判定。
func IDCT8x8(dst, src *[64]int32) error {
    if !simd.IDCT8x8(dst, src) {
        return fmt.Errorf("transform: 8x8 residual overflow: %w", h264.ErrInvalidData)
    }
    return nil
}

// Dequant4x4WithScaling 接受已转换的QP'（0..MaxScaledQP）；nil使用平坦矩阵。
func Dequant4x4WithScaling(dst, levels *[16]int32, qp int, scaling *[16]uint8) error {
	if qp < 0 || qp > MaxScaledQP {
		return fmt.Errorf("transform: 4x4 QP %d: %w", qp, h264.ErrInvalidData)
	}
	for i := 0; i < 16; i++ {
		matrix := uint8(16)
		if scaling != nil {
			matrix = scaling[i]
		}
		if matrix == 0 {
			return fmt.Errorf("transform: zero 4x4 scaling entry: %w", h264.ErrInvalidData)
		}
		mul := dequantMul(levelScale4x4[qp%6][i], qp, matrix)
		v := (int64(levels[i])*mul + 32) >> 6
		if v < -1<<31 || v > 1<<31-1 {
			return fmt.Errorf("transform: scaled 4x4 coefficient overflow: %w", h264.ErrInvalidData)
		}
		dst[i] = int32(v)
	}
	return nil
}
