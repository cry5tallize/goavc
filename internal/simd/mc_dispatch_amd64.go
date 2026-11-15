//go:build amd64 && !purego

package simd

// 初始化时确定的后端只在每块入口判断一次。静态直调保留汇编的 noescape
// 契约；若将 slice 经过函数表传递，边界 patch 和单采样输出会被迫堆分配。
func copy16Platform(dst []byte, ds int, ref []byte, rs, height int) bool {
	switch kernels.backends.MCLumaCopy {
	case AVX2:
		copy16AVX2(dst, ds, ref, rs, height)
		return true
	case SSE2:
		copy16SSE2(dst, ds, ref, rs, height)
		return true
	default:
		return false
	}
}
