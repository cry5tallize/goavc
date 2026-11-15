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

// forward8Generic is the single exact scalar implementation. Its fixed-array
// value ABI supports aliasing at the domain boundary without retaining pointers.
func forward8Generic(src [64]int32) (dst [64]int32) {
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
	return
}

// Forward8x8 is the legacy pointer adapter.
func Forward8x8(dst, src *[64]int32) { *dst = kernels.forward8(*src) }

func absForward8(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
