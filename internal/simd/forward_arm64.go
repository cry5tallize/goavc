//go:build arm64 && !purego

package simd

// 固定64字节值输入与输出，不保留外部指针，只使用基线ASIMD。
//
//go:noescape
func forward4NEON(src [16]int32) (out [16]int32)
