package simd

// AverageBlock8/16 保留逐样本(a+b+1)>>1及原地/重叠语义；stride以采样计。
// 完整矩形和别名条件不满足SIMD时使用同一精确Go公式，不读取末行padding。
func AverageBlock8(dst []byte, ds int, a, b []byte, ss, w, h int) {
	averageBlock8Native(dst, ds, a, b, ss, w, h)
}

// AverageBlock16 对完整uint16输入域精确，原生10-bit低两位不丢失。
func AverageBlock16(dst []uint16, ds int, a, b []uint16, ss, w, h int) {
	averageBlock16Native(dst, ds, a, b, ss, w, h)
}

func averageBlockGeneric[T ~uint8 | ~uint16](dst []T, ds int, a, b []T, ss, w, h int) {
	for y := range h {
		d, u, v := dst[y*ds:][:w], a[y*ss:][:w], b[y*ss:][:w]
		for x := range d {
			d[x] = T((int(u[x]) + int(v[x]) + 1) >> 1)
		}
	}
}
