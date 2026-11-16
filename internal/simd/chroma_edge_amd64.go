//go:build amd64 && !purego

package simd

// 每次只读八条线的p1..q1，写p0/q0；零阈值/全零bS不访问像素。
// 指针只在本次调用内使用；领域层先证明实际len与几何满足契约。
//
//go:noescape
func filterChroma8EdgeSSE2(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int)

// 直接命名调用使编译器能证明slice不逃逸，避免通过函数值丢失noescape信息。
// 初始化后的诊断槽同时决定实际分派；测试标签可单独切换此槽。
func filterChroma8EdgeNative(data []byte, q0, lineStep, acrossStep int, strengths [4]uint8, alpha, beta int, tc [4]int) {
	if kernels.backends.FilterChromaEdge == SSE2 {
		filterChroma8EdgeSSE2(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc)
	} else {
		filterChroma8EdgeGeneric(data, q0, lineStep, acrossStep, strengths, alpha, beta, tc)
	}
}
