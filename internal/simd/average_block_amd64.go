//go:build amd64 && !purego

package simd

// 汇编不保存输入引用，调用方已证明矩形和别名；每次访问均在有效width内。
//
//go:noescape
func averageBlock8SSE2(dst []byte, ds int, a, b []byte, ss, w, h int)

//go:noescape
func averageBlock16SSE2(dst []uint16, ds int, a, b []uint16, ss, w, h int)

func installAverageKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.backends.Average8, t.backends.Average16 = SSE2, SSE2
	}
	return t
}

func averageBlock8Native(dst []byte, ds int, a, b []byte, ss, w, h int) {
	if kernels.backends.Average8 == SSE2 && w >= 4 && vectorBlockOK(dst, ds, a, b, ss, w, h) {
		averageBlock8SSE2(dst, ds, a, b, ss, w, h)
	} else {
		averageBlockGeneric(dst, ds, a, b, ss, w, h)
	}
}

func averageBlock16Native(dst []uint16, ds int, a, b []uint16, ss, w, h int) {
	if kernels.backends.Average16 == SSE2 && w >= 4 && vectorBlockOK(dst, ds, a, b, ss, w, h) {
		averageBlock16SSE2(dst, ds, a, b, ss, w, h)
	} else {
		averageBlockGeneric(dst, ds, a, b, ss, w, h)
	}
}
