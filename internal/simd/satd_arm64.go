//go:build arm64 && !purego

package simd

// 固定64字节值参数，无外部指针；原生执行未验证，只按用户要求交叉构建。
func satd4NEON(src [16]int32) int
