//go:build !amd64 || purego

package simd

func filterLuma8EdgeNative(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int) {
	filterLuma8EdgeGeneric(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc)
}

func TryFilterLuma8Edge([]byte, int, int, int, [4]uint8, int, int, [4]int) bool {
	return false
}
