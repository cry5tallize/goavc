//go:build arm64 && !purego

package simd

//go:noescape
func wideAdd4NEON(a, b [4]int64) (out [4]int64)
