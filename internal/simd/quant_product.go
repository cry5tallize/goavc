package simd

// signedProducts4 保留有符号 int64 乘积。普通系数/乘子可装入32位时，
// 一次执行四个32×32→64乘法；Hadamard宽域或更大乘子回退原int64算术。
// 先判范围再取绝对值，避免MinInt64取负回绕。值ABI不让领域局部系数逃逸。
func signedProducts4(values, mul [4]int64) (out [4]int64) {
	if kernels.quantScalar {
		for i := range out {
			out[i] = values[i] * mul[i]
		}
		return
	}
	return signedProducts4WithBackend(values, mul, kernels.quantMultiply)
}

// 显式后端参数供变换直接parity使用，不能借全局表将SSE2请求偷换为AVX2。
func signedProducts4WithBackend(values, mul [4]int64, multiply func([4]uint64, [4]uint64) [4]uint64) (out [4]int64) {
	var a, b [4]uint64
	for i, v := range values {
		if v < -0xffffffff || v > 0xffffffff || mul[i] < 0 || mul[i] > 0xffffffff {
			for j := range out {
				out[j] = values[j] * mul[j]
			}
			return
		}
		if v < 0 {
			v = -v
		}
		a[i], b[i] = uint64(v), uint64(mul[i])
	}
	products := multiply(a, b)
	for i, v := range products {
		out[i] = int64(v)
		if values[i] < 0 {
			out[i] = -out[i]
		}
	}
	return
}
func quantMultiplyGeneric(a, b [4]uint64) (out [4]uint64) {
	for i := range out {
		out[i] = a[i] * b[i]
	}
	return
}
