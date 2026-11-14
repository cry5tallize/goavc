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

// Forward8x8 实现 High Profile 的 8×8 可分离整数变换。
// 输入为光栅顺序残差；编码侧缩放/舍入由 Quant8x8WithScaling 处理。
// 行列运算使用 int64，避免极端合法 8-bit 残差累加溢出。
func Forward8x8(dst, src *[64]int32) {
	var temp [64]int64
	for y := 0; y < 8; y++ {
		var line [8]int64
		for x := range line {
			line[x] = int64(src[y*8+x])
		}
		out := forward8Line(line)
		copy(temp[y*8:y*8+8], out[:])
	}
	for x := 0; x < 8; x++ {
		var line [8]int64
		for y := range line {
			line[y] = temp[y*8+x]
		}
		out := forward8Line(line)
		for y, v := range out {
			dst[y*8+x] = int32(v)
		}
	}
}

func forward8Line(x [8]int64) [8]int64 {
	s0, s1, s2, s3 := x[0]+x[7], x[1]+x[6], x[2]+x[5], x[3]+x[4]
	s4, s5, s6, s7 := x[3]-x[4], x[2]-x[5], x[1]-x[6], x[0]-x[7]
	b0, b1, b2, b3 := s0+s3, s1+s2, s0-s3, s1-s2
	b4, b5 := 2*s4+s5, s4-2*s5
	b6, b7 := s7-2*s6, 2*s7+s6
	return [8]int64{
		b0 + b1, b2 + (b3 >> 1), b0 - b1, b2 - (b3 >> 1),
		b4 + (b7 >> 1), b5 + b6, b5 - b6, b7 - (b4 >> 1),
	}
}

// Quant8x8WithScaling 以解码器的逆量化矩阵为基准选择量化级。
// 量化器不是码流规范的一部分；反推实际 inverse scale 保证矩阵应用和
// QP 缩放与 Dequant8x8 完全配对。nil scaling 表示 4:2:0 flat=16。
func Quant8x8WithScaling(dst, src *[64]int32, qp int, scaling *[64]uint8) error {
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
		level := (v*64 + mul/3) / mul
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
