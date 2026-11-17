//go:build amd64 && !purego

package simd

// 内部相位组合只访问已验证的w×h；b=nil不读取第二源，不保留引用。
//
//go:noescape
func mcCombine8SSE2(dst []byte, ds int, a []byte, as int, b []byte, bs, w, h int, average bool)

func installMCCombineKernels(cpu cpuFeatures, t kernelTable) kernelTable {
	if cpu.sse2 {
		t.backends.MCBlend8 = SSE2
	}
	return t
}
func mcCombine8Native(dst []byte, ds int, a []byte, as int, b []byte, bs, w, h int, average bool) {
	if kernels.backends.MCBlend8 == SSE2 && w >= 4 && h > 0 {
		mcCombine8SSE2(dst, ds, a, as, b, bs, w, h, average)
		return
	}
	mcCombineGeneric(dst, ds, a, as, b, bs, w, h, average)
}
