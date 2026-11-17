//go:build !amd64 || purego

package simd

func installMCFractionalKernels(_ cpuFeatures, t kernelTable) kernelTable { return t }
func luma8PartsNative(src []byte, rs, w, h, fx, fy int, raw *[336]int32, horizontal, vertical, diagonal []byte) bool {
	return false
}
func chroma8Native(dst []byte, ds int, src []byte, rs, w, h, fx, fy int, average bool) bool {
	return false
}
