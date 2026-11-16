//go:build !amd64 || purego

package simd

func installWeightKernels(_ cpuFeatures, t kernelTable) kernelTable { return t }
func TryWeightBlock8(dst []byte, ds int, src []byte, ss, w, h int, weight, offset int32, denom uint32) bool {
	return false
}
func TryBiWeightBlock8(dst []byte, ds int, a, b []byte, ss, w, h int, weights, offsets [2]int32, denom uint32) bool {
	return false
}
func TryWeightBlock16(dst []uint16, ds int, src []uint16, ss, w, h int, weight, offset int32, denom uint32, depth int) bool {
	return false
}
func TryBiWeightBlock16(dst []uint16, ds int, a, b []uint16, ss, w, h int, weights, offsets [2]int32, denom uint32, depth int) bool {
	return false
}
