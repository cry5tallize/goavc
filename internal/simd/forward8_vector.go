package simd

// forward8Vectorized 保持Generic的精确投影、整数除法和符号舍入，
// 仅将每组四个乘积交给显式绑定的真实后端；宽域输入回退int64乘法。
func forward8Vectorized(src [64]int32, multiply quantMultiplyFunc) (dst [64]int32) {
	var temp [64]int64
	for y := range 8 {
		for k := range 8 {
			var sum int64
			for x := 0; x < 8; x += 4 {
				var values, basis [4]int64
				for i := range 4 {
					values[i] = int64(src[y*8+x+i])
					basis[i] = forward8Basis64[k][x+i]
				}
				products := signedBasisProducts4(values, basis, multiply)
				for _, product := range products {
					sum += product
				}
			}
			temp[y*8+k] = sum
		}
	}
	for ky := range 8 {
		for kx := range 8 {
			var sum int64
			for y := 0; y < 8; y += 4 {
				var values, basis [4]int64
				for i := range 4 {
					values[i] = temp[(y+i)*8+kx]
					basis[i] = forward8Basis64[ky][y+i]
				}
				products := signedBasisProducts4(values, basis, multiply)
				for _, product := range products {
					sum += product
				}
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

func signedBasisProducts4(values, basis [4]int64, multiply quantMultiplyFunc) (products [4]int64) {
	negativeBasis := [4]bool{}
	var a, b [4]uint64
	for i := range 4 {
		v, m := values[i], basis[i]
		if m < 0 {
			negativeBasis[i], m = true, -m
		}
		if v < -0xffffffff || v > 0xffffffff || m > 0xffffffff {
			for j := range 4 {
				products[j] = values[j] * basis[j]
			}
			return
		}
		if v < 0 {
			v = -v
		}
		a[i], b[i] = uint64(v), uint64(m)
	}
	unsigned := multiply(a, b)
	for i, product := range unsigned {
		products[i] = int64(product)
		if values[i] < 0 {
			products[i] = -products[i]
		}
		if negativeBasis[i] {
			products[i] = -products[i]
		}
	}
	return
}
