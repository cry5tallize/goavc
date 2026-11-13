package transform

// SATD4x4 对残差做二维 Hadamard 并返回绝对值和/2。
// 输入为 8-bit 样本差，范围 [-255,255]；不是编解码重建所用的正变换。
func SATD4x4(src *[16]int32) int {
	var tmp [16]int32
	for y := 0; y < 4; y++ {
		i := y * 4
		a, b := src[i]+src[i+3], src[i+1]+src[i+2]
		c, d := src[i]-src[i+3], src[i+1]-src[i+2]
		tmp[i], tmp[i+1], tmp[i+2], tmp[i+3] = a+b, c+d, a-b, c-d
	}
	sum := int32(0)
	for x := 0; x < 4; x++ {
		a, b := tmp[x]+tmp[12+x], tmp[4+x]+tmp[8+x]
		c, d := tmp[x]-tmp[12+x], tmp[4+x]-tmp[8+x]
		for _, v := range [...]int32{a + b, c + d, a - b, c - d} {
			sum += max(v, -v)
		}
	}
	return int((sum + 1) >> 1)
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
