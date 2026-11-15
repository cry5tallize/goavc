//go:build amd64 && !purego

package simd

// 固定值参数/返回值各64字节，只访问各自完整数组；无外部指针或对齐要求。
//
//go:noescape
func forward4SSE2(src [16]int32) (out [16]int32)

//go:noescape
func copy16SSE2(dst []byte, ds int, ref []byte, rs, height int)

//go:noescape
func copy16AVX2(dst []byte, ds int, ref []byte, rs, height int)
