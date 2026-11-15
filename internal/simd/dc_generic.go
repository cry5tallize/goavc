package simd

// forwardLumaDCGeneric 执行4×4亮度DC Hadamard及编码侧(v+1)>>1舍入。
func forwardLumaDCGeneric(src [16]int32) (dst [16]int32) {
	var tmp [16]int64
	for i := 0; i < 4; i++ {
		a, b, c, d := int64(src[i*4]), int64(src[i*4+1]), int64(src[i*4+2]), int64(src[i*4+3])
		s01, d01, s23, d23 := a+b, a-b, c+d, c-d
		tmp[i*4], tmp[i*4+1], tmp[i*4+2], tmp[i*4+3] = s01+s23, s01-s23, d01-d23, d01+d23
	}
	for i := 0; i < 4; i++ {
		a, b, c, d := tmp[i], tmp[4+i], tmp[8+i], tmp[12+i]
		s01, d01, s23, d23 := a+b, a-b, c+d, c-d
		out := [4]int64{s01 + s23, s01 - s23, d01 - d23, d01 + d23}
		for j, v := range out {
			dst[j*4+i] = int32((v + 1) >> 1)
		}
	}
	return
}

// forwardChromaDCGeneric 执行未归一化的2×2色度Hadamard。
func forwardChromaDCGeneric(src [4]int32) (dst [4]int32) {
	a, b, c, d := int64(src[0]), int64(src[1]), int64(src[2]), int64(src[3])
	dst = [4]int32{int32(a + b + c + d), int32(a - b + c - d), int32(a + b - c - d), int32(a - b - c + d)}
	return
}

// forwardChromaDC422Generic 执行含int64中间量的H4*src*H2变换。
func forwardChromaDC422Generic(src [8]int32) (dst [8]int64) {
	var tmp [8]int64
	for y := 0; y < 4; y++ {
		a, b := int64(src[2*y]), int64(src[2*y+1])
		tmp[2*y], tmp[2*y+1] = a+b, a-b
	}
	for x := 0; x < 2; x++ {
		a, b, c, d := tmp[x], tmp[2+x], tmp[4+x], tmp[6+x]
		dst[x], dst[2+x], dst[4+x], dst[6+x] = a+b+c+d, a+b-c-d, a-b-c+d, a-b+c-d
	}
	return
}

// ForwardLumaDC调用当前绑定的亮度DC变换内核。
func ForwardLumaDC(src [16]int32) [16]int32 { return kernels.forwardLumaDC(src) }

// ForwardChromaDC调用当前绑定的420色度DC变换内核。
func ForwardChromaDC(src [4]int32) [4]int32 { return kernels.forwardChromaDC(src) }

// ForwardChromaDC422调用当前绑定的422色度DC变换内核。
func ForwardChromaDC422(src [8]int32) [8]int64 { return kernels.forwardChromaDC422(src) }

// DequantLumaDC执行I16 DC逆Hadamard、缩放、溢出检测并保留旧写入序。
func DequantLumaDC(src [16]int32, qmul int64) (dst [16]int32, written int) {
	var tmp [16]int64
	for i := range 4 {
		a, b, c, d := int64(src[i*4]), int64(src[i*4+1]), int64(src[i*4+2]), int64(src[i*4+3])
		s01, d01, s23, d23 := a+b, a-b, c+d, c-d
		row := kernels.wideAdd4([4]int64{s01, s01, d01, d01}, [4]int64{s23, -s23, -d23, d23})
		copy(tmp[i*4:i*4+4], row[:])
	}
	for i := range 4 {
		a, b, c, d := tmp[i], tmp[4+i], tmp[8+i], tmp[12+i]
		s01, d01, s23, d23 := a+b, a-b, c+d, c-d
		values := kernels.wideAdd4([4]int64{s01, s01, d01, d01}, [4]int64{s23, -s23, -d23, d23})
		products := signedProducts4(values, [4]int64{qmul, qmul, qmul, qmul})
		for j, product := range products {
			value := (product + 128) >> 8
			if value < -1<<31 || value > 1<<31-1 {
				return dst, written
			}
			dst[j*4+i] = int32(value)
			written++
		}
	}
	return dst, written
}

// DequantChromaDC执行420色度DC逆Hadamard与缩放。
func DequantChromaDC(src [4]int32, qmul int64) (dst [4]int32, written int) {
	a, b, c, d := int64(src[0]), int64(src[1]), int64(src[2]), int64(src[3])
	s01, d01, s23, d23 := a+b, a-b, c+d, c-d
	values := kernels.wideAdd4([4]int64{s01, d01, s01, d01}, [4]int64{s23, d23, -s23, -d23})
	products := signedProducts4(values, [4]int64{qmul, qmul, qmul, qmul})
	for i, product := range products {
		value := product >> 7
		if value < -1<<31 || value > 1<<31-1 {
			return dst, written
		}
		dst[i] = int32(value)
		written++
	}
	return dst, written
}

// DequantChromaDC422执行422色度DC逆Hadamard与规范舍入。
func DequantChromaDC422(src [8]int32, mul int64) (dst [8]int32, written int) {
	values := forwardChromaDC422Vectorized(src, kernels.wideAdd4)
	for base := 0; base < 8; base += 4 {
		var batch [4]int64
		copy(batch[:], values[base:base+4])
		products := signedProducts4(batch, [4]int64{mul, mul, mul, mul})
		for i, product := range products {
			value := (product + 32) >> 6
			if value < -1<<31 || value > 1<<31-1 {
				return dst, written
			}
			dst[base+i] = int32(value)
			written++
		}
	}
	return dst, written
}
