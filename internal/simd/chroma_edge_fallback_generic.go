//go:build !amd64 || purego

package simd

func filterChroma8EdgeNative(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int) {
	filterChroma8EdgeGeneric(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc)
}
