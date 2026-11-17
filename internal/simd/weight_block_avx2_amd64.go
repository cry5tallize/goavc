//go:build amd64 && !purego

package simd

// 八采样32位乘加；完整16宽且至少两行才由包装选择，窄块/单行继续SSE2。
// 输入域和bias沿用prepareBlockWeight，uint16在内核中先减32768。
//
//go:noescape
func weightBlock8AVX2(dst []byte, ds int, a, b []byte, ss, w, h int, coeff uint32, bias, offset int32, shift uint32)

//go:noescape
func weightBlock16AVX2(dst []uint16, ds int, a, b []uint16, ss, w, h int, coeff uint32, bias, offset int32, shift uint32, maximum int32)
