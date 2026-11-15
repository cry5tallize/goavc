package simd

func idct4Vectorized(src [16]int32, add wideAdd4Func) (dst [16]int32) {
	var coeff [16]int64
	for i, value := range src {
		coeff[i] = int64(value)
	}
	coeff[0] += 32
	var temp [16]int64
	for y := range 4 {
		i := y * 4
		a, b := coeff[i]+coeff[i+2], coeff[i]-coeff[i+2]
		c, d := (coeff[i+1]>>1)-coeff[i+3], coeff[i+1]+(coeff[i+3]>>1)
		row := add([4]int64{a, b, b, a}, [4]int64{d, c, -c, -d})
		copy(temp[i:i+4], row[:])
	}
	for x := range 4 {
		a, b := temp[x]+temp[8+x], temp[x]-temp[8+x]
		c, d := (temp[4+x]>>1)-temp[12+x], temp[4+x]+(temp[12+x]>>1)
		column := add([4]int64{a, b, b, a}, [4]int64{d, c, -c, -d})
		dst[x], dst[4+x], dst[8+x], dst[12+x] = int32(column[0]>>6), int32(column[1]>>6), int32(column[2]>>6), int32(column[3]>>6)
	}
	return
}

func inverse8LineVectorized(v [8]int64, add wideAdd4Func) (out [8]int64) {
	e0, e2 := v[0]+v[4], v[0]-v[4]
	e4, e6 := (v[2]>>1)-v[6], v[2]+(v[6]>>1)
	b0, b2, b4, b6 := e0+e6, e2+e4, e2-e4, e0-e6
	o1 := -v[3] + v[5] - v[7] - (v[7] >> 1)
	o3 := v[1] + v[7] - v[3] - (v[3] >> 1)
	o5 := -v[1] + v[7] + v[5] + (v[5] >> 1)
	o7 := v[3] + v[5] + v[1] + (v[1] >> 1)
	b1, b3, b5, b7 := o1+(o7>>2), o3+(o5>>2), (o3>>2)-o5, o7-(o1>>2)
	sums := add([4]int64{b0, b2, b4, b6}, [4]int64{b7, b5, b3, b1})
	diffs := add([4]int64{b6, b4, b2, b0}, [4]int64{-b1, -b3, -b5, -b7})
	out = [8]int64{sums[0], sums[1], sums[2], sums[3], diffs[0], diffs[1], diffs[2], diffs[3]}
	return
}

func idct8Vectorized(src [64]int32, add wideAdd4Func) (dst [64]int32, written int) {
	var temp [64]int64
	for y := range 8 {
		var line [8]int64
		for x := range 8 {
			line[x] = int64(src[y*8+x])
		}
		if y == 0 {
			line[0] += 32
		}
		out := inverse8LineVectorized(line, add)
		copy(temp[y*8:y*8+8], out[:])
	}
	for x := range 8 {
		var line [8]int64
		for y := range 8 {
			line[y] = temp[y*8+x]
		}
		out := inverse8LineVectorized(line, add)
		for y, value := range out {
			value >>= 6
			if value < -1<<31 || value > 1<<31-1 {
				return dst, written
			}
			dst[y*8+x] = int32(value)
			written++
		}
	}
	return dst, written
}

func forwardLumaDCVectorized(src [16]int32, add wideAdd4Func) (dst [16]int32) {
	var temp [16]int64
	for row := range 4 {
		i := row * 4
		a, b, c, d := int64(src[i]), int64(src[i+1]), int64(src[i+2]), int64(src[i+3])
		s01, d01, s23, d23 := a+b, a-b, c+d, c-d
		out := add([4]int64{s01, s01, d01, d01}, [4]int64{s23, -s23, -d23, d23})
		copy(temp[i:i+4], out[:])
	}
	for x := range 4 {
		a, b, c, d := temp[x], temp[4+x], temp[8+x], temp[12+x]
		s01, d01, s23, d23 := a+b, a-b, c+d, c-d
		out := add([4]int64{s01, s01, d01, d01}, [4]int64{s23, -s23, -d23, d23})
		for j, value := range out {
			dst[j*4+x] = int32((value + 1) >> 1)
		}
	}
	return
}

func forwardChromaDC422Vectorized(src [8]int32, add wideAdd4Func) (dst [8]int64) {
	var tmp [8]int64
	for row := range 4 {
		a, b := int64(src[2*row]), int64(src[2*row+1])
		tmp[2*row], tmp[2*row+1] = a+b, a-b
	}
	for x := range 2 {
		a, b, c, d := tmp[x], tmp[2+x], tmp[4+x], tmp[6+x]
		s01, d01, s23, d23 := a+b, a-b, c+d, c-d
		out := add([4]int64{s01, s01, d01, d01}, [4]int64{s23, -s23, -d23, d23})
		dst[x], dst[2+x], dst[4+x], dst[6+x] = out[0], out[1], out[2], out[3]
	}
	return
}

func forwardChromaDCVectorized(src [4]int32, add wideAdd4Func) (dst [4]int32) {
	a, b, c, d := int64(src[0]), int64(src[1]), int64(src[2]), int64(src[3])
	s01, d01, s23, d23 := a+b, a-b, c+d, c-d
	out := add([4]int64{s01, d01, s01, d01}, [4]int64{s23, d23, -s23, -d23})
	for i, value := range out {
		dst[i] = int32(value)
	}
	return
}
