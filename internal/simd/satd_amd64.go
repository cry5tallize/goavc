//go:build amd64 && !purego

package simd

// 固定值参数64字节，所有加载只访问该值；无外部指针或保留对象。
func satd4SSE2(src [16]int32) int
func satd4AVX2(src [16]int32) int
