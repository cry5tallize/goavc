package simd

// Quant4x4 的指针只经过直接Go调用，间接SIMD调用仅接收固定值数组，
// 因而不复制整个源块/参数块、不使领域局部系数逃逸，并允许dst==src。
func Quant4x4(dst, src *[16]int32, mul *[16]int64, shift int, divisor int64) {
	round := (int64(1) << shift) / divisor
	if kernels.quantScalar {
		for i, v := range src {
			dst[i] = quantizeProduct(int64(v)*mul[i], v < 0, round, shift)
		}
		return
	}
	for base := 0; base < 16; base += 4 {
		var v, m [4]int64
		for i := range v {
			v[i], m[i] = int64(src[base+i]), mul[base+i]
		}
		products := signedProducts4(v, m)
		for i, p := range products {
			dst[base+i] = quantizeProduct(p, v[i] < 0, round, shift)
		}
	}
}

func quantizeProduct(product int64, negative bool, round int64, shift int) int32 {
	if negative {
		product = -product
	}
	q := int32((product + round) >> shift)
	if negative {
		q = -q
	}
	return q
}

// Quant8x8 保留精确整数除法与晚期失败前缀；mul=0表示该位置矩阵非法。
func Quant8x8(dst, src *[64]int32, mul *[64]int64, divisor int64) (written, bad int) {
	if kernels.quantScalar {
		for i, v := range src {
			if mul[i] == 0 {
				return i, i
			}
			level, ok := quant8Level(int64(v)*64, v < 0, mul[i], divisor)
			if !ok {
				return i, i
			}
			dst[i] = level
		}
		return 64, 64
	}
	for base := 0; base < 64; base += 4 {
		var v [4]int64
		for i := range v {
			v[i] = int64(src[base+i])
		}
		products := signedProducts4(v, [4]int64{64, 64, 64, 64})
		for i, p := range products {
			index := base + i
			m := mul[index]
			if m == 0 {
				return index, index
			}
			level, ok := quant8Level(p, v[i] < 0, m, divisor)
			if !ok {
				return index, index
			}
			dst[index] = level
		}
	}
	return 64, 64
}

func quant8Level(product int64, negative bool, m, divisor int64) (int32, bool) {
	if negative {
		product = -product
	}
	level := (product + m/divisor) / m
	if negative {
		level = -level
	}
	return int32(level), level >= -1<<31 && level <= 1<<31-1
}

// Dequant4x4/8x8 保持光栅前缀提交；遇零乘子或int32溢出即停止写出。
// 直接借用固定数组避免整块参数/结果复制，源和目标可完全重叠。
func Dequant4x4(dst, src *[16]int32, mul *[16]int64) int {
	return dequantCoefficients(dst[:], src[:], mul[:])
}
func Dequant8x8(dst, src *[64]int32, mul *[64]int64) int {
	return dequantCoefficients(dst[:], src[:], mul[:])
}
func dequantCoefficients(dst, src []int32, mul []int64) int {
	if kernels.quantScalar {
		for i, v := range src {
			if mul[i] == 0 {
				return i
			}
			value := (int64(v)*mul[i] + 32) >> 6
			if value < -1<<31 || value > 1<<31-1 {
				return i
			}
			dst[i] = int32(value)
		}
		return len(src)
	}
	for base := 0; base < len(src); base += 4 {
		var v, m [4]int64
		for i := range v {
			v[i], m[i] = int64(src[base+i]), mul[base+i]
		}
		products := signedProducts4(v, m)
		for i, p := range products {
			index := base + i
			if m[i] == 0 {
				return index
			}
			value := (p + 32) >> 6
			if value < -1<<31 || value > 1<<31-1 {
				return index
			}
			dst[index] = int32(value)
		}
	}
	return len(src)
}

// QuantizeCoefficients 为可变长DC入口；每批先读后写，末批不越过len。
func QuantizeCoefficients(dst, src []int32, mul int64, shift int, divisor int64) {
	round := (int64(1) << shift) / divisor
	if kernels.quantScalar {
		for i, v := range src {
			dst[i] = quantizeProduct(int64(v)*mul, v < 0, round, shift)
		}
		return
	}
	for base := 0; base < len(src); base += 4 {
		n := min(4, len(src)-base)
		var v [4]int64
		for i := range n {
			v[i] = int64(src[base+i])
		}
		products := signedProducts4(v, [4]int64{mul, mul, mul, mul})
		for i := range n {
			dst[base+i] = quantizeProduct(products[i], v[i] < 0, round, shift)
		}
	}
}
