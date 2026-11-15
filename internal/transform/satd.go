package transform

import "github.com/cry5tallize/goavc/internal/simd"

// SATD4x4 对残差做二维 Hadamard 并返回绝对值和/2。
// 输入为8/10-bit样本差，范围[-1023,1023]；不是编解码重建所用的正变换。
func SATD4x4(src *[16]int32) int {
	return simd.SATD4x4(*src)
}

// SATD 按独立 4×4 子块累加代价；width/height 须为 4 的倍数，缓冲须有效。
func SATD(a []byte, as int, b []byte, bs, width, height int) int {
	sum := 0
	for by := 0; by < height; by += 4 {
		for bx := 0; bx < width; bx += 4 {
			var residual [16]int32
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					residual[y*4+x] = int32(a[(by+y)*as+bx+x]) - int32(b[(by+y)*bs+bx+x])
				}
			}
			sum += SATD4x4(&residual)
		}
	}
	return sum
}
