//go:build !amd64 || purego

package simd

func installMCCombineKernels(_ cpuFeatures, t kernelTable) kernelTable { return t }
func mcCombine8Native(dst []byte, ds int, a []byte, as int, b []byte, bs, w, h int, average bool) {
	mcCombineGeneric(dst, ds, a, as, b, bs, w, h, average)
}
