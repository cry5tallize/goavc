package transform

import "github.com/cry5tallize/goavc/internal/simd"

// IDCT4x4 做 4×4 整数逆变换（规范 8.5.12.2）。
// src 是反量化后的系数，dst 是不裁剪的残差值。
func IDCT4x4(dst *[16]int32, src *[16]int32) {
	simd.IDCT4x4(dst, src)
}

// IDCT4x4DC 是只含 DC 系数时的快速路径。
func IDCT4x4DC(dc int32) int32 {
	return simd.IDCT4x4DC(dc)
}
