package transform

import (
	"fmt"

	h264 "github.com/goavc/h264"
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

// inverse8Basis64 是 Spec: 8.5.13 逆变换单轴基底放大64的整数值。
// 行为频率、列为空间采样；行两两正交，平方范数见 inverse8Norm。
var inverse8Basis64 = [8][8]int64{
	{64, 64, 64, 64, 64, 64, 64, 64},
	{96, 80, 48, 24, -24, -48, -80, -96},
	{64, 32, -32, -64, -64, -32, 32, 64},
	{80, -24, -96, -48, 48, 96, 24, -80},
	{64, -64, -64, 64, 64, -64, -64, 64},
	{48, -96, 24, 80, -80, -24, 96, -48},
	{32, -64, 64, -32, -32, 64, -64, 32},
	{24, -48, 80, -96, 96, -80, 48, -24},
}
var inverse8Norm = [8]int64{32768, 36992, 20480, 36992, 32768, 36992, 20480, 36992}

// Forward8x8 将[-255,255]的8-bit残差投影到规范逆变换的正交基。
// 输入输出按光栅顺序，可原地调用；不使用浮点近似或另一份逆变换。
// IDCT输出为 A*C*Aᵀ/64，故 C=64*Aᵀ*src*A/(各轴平方范数)。
// 基底放大64后分子系数为2^18；合法残差的全部中间量在int64范围内。
func Forward8x8(dst, src *[64]int32) {
	var temp [64]int64
	for y := 0; y < 8; y++ {
		for k := 0; k < 8; k++ {
			for x := 0; x < 8; x++ {
				temp[y*8+k] += int64(src[y*8+x]) * inverse8Basis64[k][x]
			}
		}
	}
	for ky := 0; ky < 8; ky++ {
		for kx := 0; kx < 8; kx++ {
			var sum int64
			for y := 0; y < 8; y++ {
				sum += temp[y*8+kx] * inverse8Basis64[ky][y]
			}
			numerator, denominator := sum*(1<<18), inverse8Norm[ky]*inverse8Norm[kx]
			value := (absForward(numerator) + denominator/2) / denominator
			if numerator < 0 {
				value = -value
			}
			dst[ky*8+kx] = int32(value)
		}
	}
}

func absForward(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

// Quant8x8WithScaling 以解码器的逆量化矩阵为基准选择量化级。
// 量化器不是码流规范的一部分；反推实际 inverse scale 保证矩阵应用和
// QP 缩放与 Dequant8x8 完全配对。nil scaling 表示 4:2:0 flat=16。
func Quant8x8WithScaling(dst, src *[64]int32, qp int, scaling *[64]uint8) error {
	return quant8x8WithScaling(dst, src, qp, scaling, 3)
}

// QuantInter8x8WithScaling 与4×4帧间路径采用相同的1/6死区。
func QuantInter8x8WithScaling(dst, src *[64]int32, qp int, scaling *[64]uint8) error {
	return quant8x8WithScaling(dst, src, qp, scaling, 6)
}

func quant8x8WithScaling(dst, src *[64]int32, qp int, scaling *[64]uint8, divisor int64) error {
	if qp < 0 || qp > MaxQP {
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

func Dequant8x8(dst, levels *[64]int32, qp int, scaling *[64]uint8) error {
	if qp < 0 || qp > 51 {
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

func inverse8Line(v [8]int64) [8]int64 {
	e0, e2 := v[0]+v[4], v[0]-v[4]
	e4, e6 := (v[2]>>1)-v[6], v[2]+(v[6]>>1)
	b0, b2, b4, b6 := e0+e6, e2+e4, e2-e4, e0-e6
	o1 := -v[3] + v[5] - v[7] - (v[7] >> 1)
	o3 := v[1] + v[7] - v[3] - (v[3] >> 1)
	o5 := -v[1] + v[7] + v[5] + (v[5] >> 1)
	o7 := v[3] + v[5] + v[1] + (v[1] >> 1)
	b1, b3, b5, b7 := o1+(o7>>2), o3+(o5>>2), (o3>>2)-o5, o7-(o1>>2)
	return [8]int64{b0 + b7, b2 + b5, b4 + b3, b6 + b1, b6 - b1, b4 - b3, b2 - b5, b0 - b7}
}

// IDCT8x8 按光栅布局先水平、再垂直变换；临时量使用 int64。
// Spec: 8.5.13。保留算术右移顺序，最后一次性进行 +32/64 舍入。
func IDCT8x8(dst, src *[64]int32) error {
	var temp [64]int64
	for y := 0; y < 8; y++ {
		var line [8]int64
		for x := 0; x < 8; x++ {
			line[x] = int64(src[y*8+x])
		}
		if y == 0 {
			line[0] += 32
		}
		out := inverse8Line(line)
		copy(temp[y*8:y*8+8], out[:])
	}
	for x := 0; x < 8; x++ {
		var line [8]int64
		for y := 0; y < 8; y++ {
			line[y] = temp[y*8+x]
		}
		out := inverse8Line(line)
		for y, v := range out {
			v >>= 6
			if v < -1<<31 || v > 1<<31-1 {
				return fmt.Errorf("transform: 8x8 residual overflow: %w", h264.ErrInvalidData)
			}
			dst[y*8+x] = int32(v)
		}
	}
	return nil
}

func Dequant4x4WithScaling(dst, levels *[16]int32, qp int, scaling *[16]uint8) error {
	if qp < 0 || qp > 51 {
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
