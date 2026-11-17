package simd

// idct4Generic is the exact int64 scalar 4x4 inverse transform.
func idct4Generic(src [16]int32) (dst [16]int32) {
	var temp [16]int64
	inverse4Rows(&temp, &src)
	for x := range 4 {
		dst[x], dst[4+x], dst[8+x], dst[12+x] = inverse4Column(&temp, x)
	}
	return
}

func inverse4Rows(temp *[16]int64, src *[16]int32) {
	for y := range 4 {
		i := y * 4
		v0, v1, v2, v3 := int64(src[i]), int64(src[i+1]), int64(src[i+2]), int64(src[i+3])
		if y == 0 {
			v0 += 32
		}
		a, b := v0+v2, v0-v2
		c, d := (v1>>1)-v3, v1+(v3>>1)
		temp[i], temp[i+1], temp[i+2], temp[i+3] = a+d, b+c, b-c, a-d
	}
}

func inverse4Column(temp *[16]int64, x int) (int32, int32, int32, int32) {
	a, b := temp[x]+temp[8+x], temp[x]-temp[8+x]
	c, d := (temp[4+x]>>1)-temp[12+x], temp[4+x]+(temp[12+x]>>1)
	return int32((a + d) >> 6), int32((b + c) >> 6), int32((b - c) >> 6), int32((a - d) >> 6)
}

// IDCT4x4 is the legacy pointer adapter.
func IDCT4x4(dst, src *[16]int32) { *dst = kernels.idct4(*src) }

func IDCT4x4DC(dc int32) int32 {
	return int32((int64(dc) + 32) >> 6)
}
