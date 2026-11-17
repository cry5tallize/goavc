package simd

// idct8Generic performs the exact int64 8x8 inverse transform. `written`
// counts successful stores in the original column-major iteration order.
func idct8Generic(src [64]int32) (dst [64]int32, written int) {
	var temp [64]int64
	inverse8Rows(&temp, &src)
	for x := range 8 {
		var line [8]int64
		for y := range 8 {
			line[y] = temp[y*8+x]
		}
		out := inverse8Line(line)
		for y, v := range out {
			v >>= 6
			if v < -1<<31 || v > 1<<31-1 {
				return dst, written
			}
			dst[y*8+x] = int32(v)
			written++
		}
	}
	return dst, written
}

func inverse8Rows(temp *[64]int64, src *[64]int32) {
	for y := range 8 {
		var line [8]int64
		for x := range 8 {
			line[x] = int64(src[y*8+x])
		}
		if y == 0 {
			line[0] += 32
		}
		out := inverse8Line(line)
		copy(temp[y*8:y*8+8], out[:])
	}
}

// IDCT8x8通过绑定的值ABI内核并保持原先的列优先部分写入顺序。
func IDCT8x8(dst, src *[64]int32) bool {
	out, written := kernels.idct8(*src)
	for n := 0; n < written; n++ {
		x, y := n/8, n%8
		dst[y*8+x] = out[y*8+x]
	}
	return written == 64
}

func inverse8Line(v [8]int64) [8]int64 {
	e0, e2 := v[0]+v[4], v[0]-v[4]
	e4, e6 := (v[2]>>1)-v[6], v[2]+(v[6]>>1)
	b0, b2, b4, b6 := e0+e6, e2+e4, e2-e4, e0-e6
	o1 := -v[3] + v[5] - v[7] - (v[7] >> 1)
	o3 := v[1] + v[7] - v[3] - (v[3] >> 1)
	o5 := -v[1] + v[7] + v[5] + (v[5] >> 1)
	o7 := v[3] + v[5] + v[1] + (v[1] >> 1)
	b1, b3, b5, b7 := o1+(o7>>2), o3+(o5>>2), (o3>>2)-o5, o7-(o1>>2)
	return [8]int64{b0 + b7, b2 + b5, b4 + b3, b6 + b1, b6 - b1, b4 - b3, b2 - b5, b0 - b7}
}
