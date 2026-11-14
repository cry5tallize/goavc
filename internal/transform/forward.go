package transform

import (
	"fmt"
	h264 "github.com/goavc/h264"
)

// Forward4x4 是 8-bit 残差的可分离整数变换；输入须在 [-255,255]。
// 行列基底 C={1111;21-1-2;1-1-11;1-22-1}，缩放合并到量化中。
// 解码语义见 Spec: 8.5.12；编码侧舍入策略不属于规范要求。
func Forward4x4(dst, src *[16]int32) {
	var tmp [16]int32
	for y := 0; y < 4; y++ {
		i := y * 4
		a, b := src[i]+src[i+3], src[i+1]+src[i+2]
		c, d := src[i]-src[i+3], src[i+1]-src[i+2]
		tmp[i], tmp[i+1], tmp[i+2], tmp[i+3] = a+b, 2*c+d, a-b, c-2*d
	}
	for x := 0; x < 4; x++ {
		a, b := tmp[x]+tmp[12+x], tmp[4+x]+tmp[8+x]
		c, d := tmp[x]-tmp[12+x], tmp[4+x]-tmp[8+x]
		dst[x], dst[4+x], dst[8+x], dst[12+x] = a+b, 2*c+d, a-b, c-2*d
	}
}

// ForwardLumaDC 把 16 个子块 DC 变为 I16 Hadamard 系数，输入输出为光栅顺序。
// Hadamard 增益与逆路径 DequantLumaDC 对偶；(v+1)>>1 是编码侧确定性舍入。
func ForwardLumaDC(dst, src *[16]int32) {
	var tmp [16]int32
	for y := 0; y < 4; y++ {
		i := 4 * y
		a, b, c, d := src[i], src[i+1], src[i+2], src[i+3]
		tmp[i], tmp[i+1], tmp[i+2], tmp[i+3] = a+b+c+d, a+b-c-d, a-b-c+d, a-b+c-d
	}
	for x := 0; x < 4; x++ {
		a, b, c, d := tmp[x], tmp[4+x], tmp[8+x], tmp[12+x]
		dst[x], dst[4+x], dst[8+x], dst[12+x] = (a+b+c+d+1)>>1, (a+b-c-d+1)>>1, (a-b-c+d+1)>>1, (a-b+c-d+1)>>1
	}
}

// ForwardChromaDC 为 4:2:0 色度的 2×2 Hadamard，不做归一化。
func ForwardChromaDC(dst, src *[4]int32) {
	a, b, c, d := src[0], src[1], src[2], src[3]
	*dst = [4]int32{a + b + c + d, a - b + c - d, a + b - c - d, a - b - c + d}
}

var quantScale = [6][3]int64{{13107, 8066, 5243}, {11916, 7490, 4660}, {10082, 6554, 4194}, {9362, 5825, 3647}, {8192, 5243, 3355}, {7282, 4559, 2893}}

// Quant4x4 使用平坦矩阵、intra 死区 1/3 的定点量化；QP 必须在 0..51。
// DC 块需使用 QuantDC，不能把 Hadamard 系数当普通 4×4 处理。
func Quant4x4(dst, src *[16]int32, qp int) error {
	return Quant4x4WithScaling(dst, src, qp, nil)
}

// Quant4x4WithScaling 在原量化乘子上施加16/matrix，nil保持flat逐比特一致。
// 矩阵为光栅顺序；所有矩阵项先验证再写输出，非法输入不产生半块结果。
func Quant4x4WithScaling(dst, src *[16]int32, qp int, scaling *[16]uint8) error {
	if qp < 0 || qp > 51 {
		return fmt.Errorf("transform: quant qp %d: %w", qp, h264.ErrInvalidData)
	}
	if scaling != nil {
		for _, v := range scaling {
			if v == 0 {
				return fmt.Errorf("transform: zero 4x4 quant scaling entry: %w", h264.ErrInvalidData)
			}
		}
	}
	for i, v := range src {
		class := (i / 4 % 2) + (i % 2)
		mul := quantScale[qp%6][class]
		if scaling != nil {
			mul = (mul*16 + int64(scaling[i])/2) / int64(scaling[i])
		}
		dst[i] = quantize(v, mul, 15+qp/6)
	}
	return nil
}

// QuantDC 量化已变换的 I16 或色度 DC，额外一位吸收 Hadamard 缩放。
func QuantDC(dst, src []int32, qp int) error {
	return QuantDCWithScaling(dst, src, qp, 16)
}

// QuantDCWithScaling 对Hadamard DC仅使用对应4×4矩阵的首项。
func QuantDCWithScaling(dst, src []int32, qp int, scale uint8) error {
	if qp < 0 || qp > 51 || len(dst) < len(src) {
		return fmt.Errorf("transform: invalid DC quantization: %w", h264.ErrInvalidData)
	}
	if scale == 0 {
		return fmt.Errorf("transform: zero DC quant scaling entry: %w", h264.ErrInvalidData)
	}
	for i, v := range src {
		dst[i] = quantize(v, (quantScale[qp%6][0]*16+int64(scale)/2)/int64(scale), 16+qp/6)
	}
	return nil
}

func quantize(v int32, mul int64, shift int) int32 {
	a := int64(v)
	if a < 0 {
		a = -a
	}
	q := int32((a*mul + (int64(1)<<shift)/3) >> shift)
	if v < 0 {
		return -q
	}
	return q
}
