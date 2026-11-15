package simd

// 唯一的SAD标量算法；通用矩形、purego及架构后端的尾部共用此语义。
func sadGeneric[T byte | uint16](a []T, as int, b []T, bs, width, height int) int {
	sum := 0
	for y := range height {
		for x := range width {
			d := int(a[y*as+x]) - int(b[y*bs+x])
			sum += max(d, -d)
		}
	}
	return sum
}
