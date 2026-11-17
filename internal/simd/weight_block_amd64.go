//go:build amd64 && !purego

package simd

// 四样本批量乘加、算术移位、offset和Clip1在一次调用内完成。
// b=nil表示单向，权重高16位为0；只访问完整的4样本组，不保留引用。
//
//go:noescape
func weightBlock8SSE2(dst []byte, ds int, a, b []byte, ss, w, h int, coeff uint32, bias, offset int32, shift uint32)

//go:noescape
func weightBlock16SSE2(dst []uint16, ds int, a, b []uint16, ss, w, h int, coeff uint32, bias, offset int32, shift uint32, maximum int32)

func installWeightKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.backends.Weight8, t.backends.Weight16 = SSE2, SSE2
	}
	// AVX2诊断表示16宽且至少两行的槽；其他宽度及单行仍执行SSE2。
	if cpu.sse2 && cpu.avx2 {
		t.backends.Weight8, t.backends.Weight16 = AVX2, AVX2
	}
	return t
}

// TryWeightBlock8只在范围、完整矩形及别名均满足条件时执行；false保留原Go路径。
func TryWeightBlock8(dst []byte, ds int, src []byte, ss, w, h int, weight, offset int32, denom uint32) bool {
	if (kernels.backends.Weight8 != SSE2 && kernels.backends.Weight8 != AVX2) || w < 4 || w%4 != 0 {
		return false
	}
	p, ok := prepareBlockWeight([2]int32{weight, 0}, [2]int32{offset, 0}, denom, 8, false, false)
	if !ok || !vectorSingleBlockOK(dst, ds, src, ss, w, h) {
		return false
	}
	if kernels.backends.Weight8 == AVX2 && w == 16 && h >= 2 {
		weightBlock8AVX2(dst, ds, src, nil, ss, w, h, p.coeff, p.bias, p.offset, p.shift)
	} else {
		weightBlock8SSE2(dst, ds, src, nil, ss, w, h, p.coeff, p.bias, p.offset, p.shift)
	}
	return true
}

// TryBiWeightBlock8的offset先逐位深缩放再平均，与标量顺序一致。
func TryBiWeightBlock8(dst []byte, ds int, a, b []byte, ss, w, h int, weights, offsets [2]int32, denom uint32) bool {
	if (kernels.backends.Weight8 != SSE2 && kernels.backends.Weight8 != AVX2) || w < 4 || w%4 != 0 {
		return false
	}
	p, ok := prepareBlockWeight(weights, offsets, denom, 8, true, false)
	if !ok || !vectorBlockOK(dst, ds, a, b, ss, w, h) {
		return false
	}
	if kernels.backends.Weight8 == AVX2 && w == 16 && h >= 2 {
		weightBlock8AVX2(dst, ds, a, b, ss, w, h, p.coeff, p.bias, p.offset, p.shift)
	} else {
		weightBlock8SSE2(dst, ds, a, b, ss, w, h, p.coeff, p.bias, p.offset, p.shift)
	}
	return true
}

// TryWeightBlock16保留uint16全输入域；只向量化8/10-bit输出，其他位深原路计算。
func TryWeightBlock16(dst []uint16, ds int, src []uint16, ss, w, h int, weight, offset int32, denom uint32, depth int) bool {
	if (kernels.backends.Weight16 != SSE2 && kernels.backends.Weight16 != AVX2) || w < 4 || w%4 != 0 {
		return false
	}
	p, ok := prepareBlockWeight([2]int32{weight, 0}, [2]int32{offset, 0}, denom, depth, false, true)
	if !ok || !vectorSingleBlockOK(dst, ds, src, ss, w, h) {
		return false
	}
	if kernels.backends.Weight16 == AVX2 && w == 16 && h >= 2 {
		weightBlock16AVX2(dst, ds, src, nil, ss, w, h, p.coeff, p.bias, p.offset, p.shift, 1<<depth-1)
	} else {
		weightBlock16SSE2(dst, ds, src, nil, ss, w, h, p.coeff, p.bias, p.offset, p.shift, 1<<depth-1)
	}
	return true
}

func TryBiWeightBlock16(dst []uint16, ds int, a, b []uint16, ss, w, h int, weights, offsets [2]int32, denom uint32, depth int) bool {
	if (kernels.backends.Weight16 != SSE2 && kernels.backends.Weight16 != AVX2) || w < 4 || w%4 != 0 {
		return false
	}
	p, ok := prepareBlockWeight(weights, offsets, denom, depth, true, true)
	if !ok || !vectorBlockOK(dst, ds, a, b, ss, w, h) {
		return false
	}
	if kernels.backends.Weight16 == AVX2 && w == 16 && h >= 2 {
		weightBlock16AVX2(dst, ds, a, b, ss, w, h, p.coeff, p.bias, p.offset, p.shift, 1<<depth-1)
	} else {
		weightBlock16SSE2(dst, ds, a, b, ss, w, h, p.coeff, p.bias, p.offset, p.shift, 1<<depth-1)
	}
	return true
}
