package simd

// 融合只在完整矩形上改变存储顺序；否则调用方保留旧变换/逐行Add的错误前缀。
func TryIDCTAdd4x4(dst []byte, ds int, src *[16]int32) bool {
	if kernels.backends.IDCT4x4 != Generic || !blockFits(len(dst), ds, 4, 4) {
		return false
	}
	idctAdd4(dst, ds, src, 255)
	return true
}
func TryIDCTAdd4x4High(dst []uint16, ds int, src *[16]int32) bool {
	if kernels.backends.IDCT4x4 != Generic || !blockFits(len(dst), ds, 4, 4) {
		return false
	}
	idctAdd4(dst, ds, src, 1023)
	return true
}
func addResidualPixel[T ~byte | ~uint16](p T, r int32, limit int64) T {
	return T(max(int64(0), min(limit, int64(p)+int64(r))))
}

func idctAdd4[T ~byte | ~uint16](dst []T, ds int, src *[16]int32, limit int64) {
	var temp [16]int64
	inverse4Rows(&temp, src)
	for x := range 4 {
		a, b, c, d := inverse4Column(&temp, x)
		dst[x] = addResidualPixel(dst[x], a, limit)
		dst[ds+x] = addResidualPixel(dst[ds+x], b, limit)
		dst[2*ds+x] = addResidualPixel(dst[2*ds+x], c, limit)
		dst[3*ds+x] = addResidualPixel(dst[3*ds+x], d, limit)
	}
}

// |coeff|<=2^24时，inverse8Line每遍保守增益<8，两遍/64后残差远在int32内。
// 宽域保留原IDCT8x8的溢出与部分系数写入语义；像素只有成功才由调用方修改。
func idct8FusionSafe(src *[64]int32) bool {
	for _, v := range src {
		if v < -1<<24 || v > 1<<24 {
			return false
		}
	}
	return true
}
func TryIDCTAdd8x8(dst []byte, ds int, src *[64]int32) bool {
	if kernels.backends.IDCT8x8 != Generic || !blockFits(len(dst), ds, 8, 8) || !idct8FusionSafe(src) {
		return false
	}
	idctAdd8(dst, ds, src, 255)
	return true
}
func TryIDCTAdd8x8High(dst []uint16, ds int, src *[64]int32) bool {
	if kernels.backends.IDCT8x8 != Generic || !blockFits(len(dst), ds, 8, 8) || !idct8FusionSafe(src) {
		return false
	}
	idctAdd8(dst, ds, src, 1023)
	return true
}
func idctAdd8[T ~byte | ~uint16](dst []T, ds int, src *[64]int32, limit int64) {
	var temp [64]int64
	inverse8Rows(&temp, src)
	for x := range 8 {
		var line [8]int64
		for y := range 8 {
			line[y] = temp[y*8+x]
		}
		out := inverse8Line(line)
		for y, v := range out {
			dst[y*ds+x] = addResidualPixel(dst[y*ds+x], int32(v>>6), limit)
		}
	}
}
