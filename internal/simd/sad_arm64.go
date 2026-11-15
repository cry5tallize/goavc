//go:build arm64 && !purego

package simd

// 首版NEON快路宽度精确为16，其他矩形复用唯一Generic，不增加媒体拒绝。
// 交叉构建不证明原生正确性/性能；原生parity和基准入口保留待runner执行。
func sad8NEON(a []byte, as int, b []byte, bs, width, height int) int {
	if width == 16 && height > 0 {
		return sad8x16NEON(a, as, b, bs, height)
	}
	return sadGeneric(a, as, b, bs, width, height)
}

func sad16NEON(a []uint16, as int, b []uint16, bs, width, height int) int {
	if width == 16 && height > 0 {
		return sad16x16NEON(a, as, b, bs, height)
	}
	return sadGeneric(a, as, b, bs, width, height)
}

// 每条byte行仅读16字节；uint16行仅读两个独立16字节向量。均无对齐要求。
// 不保留Go指针；stride以采样计，uint16在汇编入口转字节。
//
//go:noescape
func sad8x16NEON(a []byte, as int, b []byte, bs, height int) int

//go:noescape
func sad16x16NEON(a []uint16, as int, b []uint16, bs, height int) int
