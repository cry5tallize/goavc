package simd

import "unsafe"

// 矩形边界先除后乘，未通过的旧调用仍由标量入口按原规则处理。
func blockFits(n, stride, w, h int) bool {
	return w > 0 && h > 0 && stride >= w && n >= w && h-1 <= (n-w)/stride
}

// unsafe仅用于比较同一调用中切片实际访问范围的地址，不构造或解引用新指针。
// 几何已经过blockFits验证；局部范围乘法不超过真实slice长度。精确原地允许，
// 部分重叠回到原逐像素顺序，避免向量预读改变旧入口的别名语义。
func blockIndependent[T ~uint8 | ~uint16](dst []T, ds int, src []T, ss, w, h int) bool {
	if &dst[0] == &src[0] && ds == ss {
		return true
	}
	var sample T
	unit := unsafe.Sizeof(sample)
	d, s := uintptr(unsafe.Pointer(unsafe.SliceData(dst))), uintptr(unsafe.Pointer(unsafe.SliceData(src)))
	dn, sn := uintptr((h-1)*ds+w)*unit, uintptr((h-1)*ss+w)*unit
	return d >= s+sn || s >= d+dn
}

func vectorBlockOK[T ~uint8 | ~uint16](dst []T, ds int, a, b []T, ss, w, h int) bool {
	return blockFits(len(dst), ds, w, h) && blockFits(len(a), ss, w, h) && blockFits(len(b), ss, w, h) &&
		blockIndependent(dst, ds, a, ss, w, h) && blockIndependent(dst, ds, b, ss, w, h)
}
