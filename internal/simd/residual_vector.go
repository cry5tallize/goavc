package simd

type residualVector func([8]int32, [8]int32) [8]int32

// 限幅残差不会改变最终 Clip1，且使每一 lane 的加法不会溢出。
func residualAddVector[T ~byte | ~uint16](d []T, stride int, r []int32, rs, w, h, limit int, add residualVector) {
	for y := range h {
		x := 0
		for x+4 <= w {
			n := min(8, w-x)
			var a, b [8]int32
			for i := range n {
				a[i] = int32(d[y*stride+x+i])
				b[i] = max(-int32(limit), min(int32(limit), r[y*rs+x+i]))
			}
			c := add(a, b)
			for i := range n {
				d[y*stride+x+i] = T(max(0, min(int32(limit), c[i])))
			}
			x += n
		}
		for ; x < w; x++ {
			v := int64(d[y*stride+x]) + int64(r[y*rs+x])
			d[y*stride+x] = T(max(0, min(int64(limit), v)))
		}
	}
}
func residualSubVector[T ~byte | ~uint16](d []int32, ds int, s []T, ss int, p []T, ps, w, h int, add residualVector) {
	for y := range h {
		x := 0
		for x+4 <= w {
			n := min(8, w-x)
			var a, b [8]int32
			for i := range n {
				a[i] = int32(s[y*ss+x+i])
				b[i] = -int32(p[y*ps+x+i])
			}
			c := add(a, b)
			for i := range n {
				d[y*ds+x+i] = c[i]
			}
			x += n
		}
		for ; x < w; x++ {
			d[y*ds+x] = int32(s[y*ss+x]) - int32(p[y*ps+x])
		}
	}
}
