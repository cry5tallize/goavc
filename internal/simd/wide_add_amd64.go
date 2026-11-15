//go:build amd64 && !purego

package simd

//go:noescape
func wideAdd4SSE2(a, b [4]int64) (out [4]int64)

//go:noescape
func wideAdd4AVX2(a, b [4]int64) (out [4]int64)
