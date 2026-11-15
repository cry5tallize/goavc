//go:build amd64 && !purego

package simd

// AVX2快路的width精确为16。byte一次拼入两条独立16字节行，
// 不将单行扩大到32字节；uint16每行16采样本就有32字节。
// 其他宽度使用SSE2的同语义窄加载/标量尾部，报告时必须区分该覆盖域。
func sad8AVX2(a []byte, as int, b []byte, bs, width, height int) int {
	if width == 16 && height > 0 {
		return sad8x16AVX2(a, as, b, bs, height)
	}
	return sad8SSE2(a, as, b, bs, width, height)
}

func sad16AVX2(a []uint16, as int, b []uint16, bs, width, height int) int {
	if width == 16 && height > 0 {
		return sad16x16AVX2(a, as, b, bs, height)
	}
	return sad16SSE2(a, as, b, bs, width, height)
}

// 只读输入且不保留指针；height>0，宽度固定16，末行无需padding。
//
//go:noescape
func sad8x16AVX2(a []byte, as int, b []byte, bs, height int) int

//go:noescape
func sad16x16AVX2(a []uint16, as int, b []uint16, bs, height int) int
