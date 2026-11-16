//go:build !amd64 || purego

package simd

func installAverageKernels(_ cpuFeatures, t kernelTable) kernelTable { return t }
func averageBlock8Native(dst []byte, ds int, a, b []byte, ss, w, h int) {
	averageBlockGeneric(dst, ds, a, b, ss, w, h)
}
func averageBlock16Native(dst []uint16, ds int, a, b []uint16, ss, w, h int) {
	averageBlockGeneric(dst, ds, a, b, ss, w, h)
}
