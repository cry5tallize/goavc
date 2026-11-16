package simd

// AddResidualDC8 将已经完成逆变换舍入的常量残差加入有效矩形。
// stride以采样计；与普通残差相同，int64加法保留int32极值的Clip1语义。
func AddResidualDC8(dst []byte, stride, w, h int, dc int32) {
	residualAddDC(dst, stride, w, h, dc, 255)
}

// AddResidualDC16 是原生10-bit入口，不改变低两位或行填充。
func AddResidualDC16(dst []uint16, stride, w, h int, dc int32) {
	residualAddDC(dst, stride, w, h, dc, 1023)
}

func residualAddDC[T ~byte | ~uint16](dst []T, stride, w, h int, dc int32, limit int64) {
	if dc == 0 {
		return
	}
	for y := range h {
		row := dst[y*stride:][:w]
		for x := range row {
			row[x] = T(max(0, min(limit, int64(row[x])+int64(dc))))
		}
	}
}
