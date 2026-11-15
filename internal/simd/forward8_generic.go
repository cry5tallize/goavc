package simd

var forward8Basis64 = [8][8]int64{
	{64, 64, 64, 64, 64, 64, 64, 64},
	{96, 80, 48, 24, -24, -48, -80, -96},
	{64, 32, -32, -64, -64, -32, 32, 64},
	{80, -24, -96, -48, 48, 96, 24, -80},
	{64, -64, -64, 64, 64, -64, -64, 64},
	{48, -96, 24, 80, -80, -24, 96, -48},
	{32, -64, 64, -32, -32, 64, -64, 32},
	{24, -48, 80, -96, 96, -80, 48, -24},
}

var forward8Norm = [8]int64{32768, 36992, 20480, 36992, 32768, 36992, 20480, 36992}

// Forward8x8 是 Spec: 8.5.13 的共享整数正变换 Generic 实现。
// 输入输出按光栅顺序，可原地调用；int64 中间域保证合法8/10-bit残差不溢出。
func Forward8x8(dst, src *[64]int32) {
	var temp [64]int64
	for y := range 8 {
		for k := range 8 {
			for x := range 8 {
				temp[y*8+k] += int64(src[y*8+x]) * forward8Basis64[k][x]
			}
		}
	}
	for ky := range 8 {
		for kx := range 8 {
			var sum int64
			for y := range 8 {
				sum += temp[y*8+kx] * forward8Basis64[ky][y]
			}
			numerator, denominator := sum*(1<<18), forward8Norm[ky]*forward8Norm[kx]
			value := (absForward8(numerator) + denominator/2) / denominator
			if numerator < 0 {
				value = -value
			}
			dst[ky*8+kx] = int32(value)
		}
	}
}

func absForward8(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
