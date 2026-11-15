package simd

// 两轴整数基底C={1111;21-1-2;1-1-11;1-22-1}，与原领域实现逐项相同。
// 有效残差±1023时单轴绝对值<=6138，双轴<=36828，32位无需饱和。
func forward4Generic(src [16]int32) (dst [16]int32) {
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
	return
}
