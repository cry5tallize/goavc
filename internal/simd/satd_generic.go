package simd

// 唯一Hadamard标量实现。没有中间舍入；最终绝对值和仍按int32旧语义归约。
func satd4Generic(src [16]int32) int {
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
