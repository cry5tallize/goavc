package simd

// IDCT4x4 执行 Spec: 8.5.12.2 的整数逆变换。
// 临时值使用 int64；输入输出可别名，结果不裁剪。
func IDCT4x4(dst *[16]int32, src *[16]int32) {
	var coeff [16]int64
	for i, v := range src {
		coeff[i] = int64(v)
	}
	coeff[0] += 32
	var temp [16]int64
	for y := range 4 {
		i := y * 4
		a, b := coeff[i]+coeff[i+2], coeff[i]-coeff[i+2]
		c, d := (coeff[i+1]>>1)-coeff[i+3], coeff[i+1]+(coeff[i+3]>>1)
		temp[i], temp[i+1], temp[i+2], temp[i+3] = a+d, b+c, b-c, a-d
	}
	for x := range 4 {
		a, b := temp[x]+temp[8+x], temp[x]-temp[8+x]
		c, d := (temp[4+x]>>1)-temp[12+x], temp[4+x]+(temp[12+x]>>1)
		dst[x], dst[4+x], dst[8+x], dst[12+x] = int32((a+d)>>6), int32((b+c)>>6), int32((b-c)>>6), int32((a-d)>>6)
	}
}

func IDCT4x4DC(dc int32) int32 {
	return int32((int64(dc) + 32) >> 6)
}
