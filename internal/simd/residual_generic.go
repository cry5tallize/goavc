package simd

// AddResidual8 和 AddResidual16 的 stride 均以对应切片的元素数为单位。
// 每行恰好访问 width 个元素，调用者保证长度和区域有效。
func AddResidual8(dst []byte, stride int, residual []int32, rs, width, height int) {
	if kernels.residualScalar {
		residualAddScalar(dst, stride, residual, rs, width, height, 255)
		return
	}
	residualAddVector(dst, stride, residual, rs, width, height, 255, kernels.residualVector)
}
func AddResidual16(dst []uint16, stride int, residual []int32, rs, width, height int) {
	if kernels.residualScalar {
		residualAddScalar(dst, stride, residual, rs, width, height, 1023)
		return
	}
	residualAddVector(dst, stride, residual, rs, width, height, 1023, kernels.residualVector)
}

// Subtract8/16 对两个采样矩形逐元素相减，结果为有符号 int32。
// 各 stride 以对应切片的元素数计；各行仅访问 width 个元素。
func Subtract8(dst []int32, ds int, src []byte, ss int, pred []byte, ps, width, height int) {
	if kernels.residualScalar {
		residualSubScalar(dst, ds, src, ss, pred, ps, width, height)
		return
	}
	residualSubVector(dst, ds, src, ss, pred, ps, width, height, kernels.residualVector)
}
func Subtract16(dst []int32, ds int, src []uint16, ss int, pred []uint16, ps, width, height int) {
	if kernels.residualScalar {
		residualSubScalar(dst, ds, src, ss, pred, ps, width, height)
		return
	}
	residualSubVector(dst, ds, src, ss, pred, ps, width, height, kernels.residualVector)
}

func residualVectorGeneric(a, b [8]int32) (out [8]int32) {
	for i := range out {
		out[i] = a[i] + b[i]
	}
	return out
}

// 标量路径不为未启用的向量槽做gather；模式在初始化确定，每块仅一次快路判断。
func residualAddScalar[T ~byte | ~uint16](dst []T, ds int, residual []int32, rs, w, h, limit int) {
	for y := range h {
		d := dst[y*ds:][:w]
		r := residual[y*rs:][:w]
		for x := range d {
			v := int64(d[x]) + int64(r[x])
			d[x] = T(max(0, min(int64(limit), v)))
		}
	}
}
func residualSubScalar[T ~byte | ~uint16](dst []int32, ds int, src []T, ss int, pred []T, ps, w, h int) {
	for y := range h {
		d := dst[y*ds:][:w]
		s := src[y*ss:][:w]
		p := pred[y*ps:][:w]
		for x := range d {
			d[x] = int32(s[x]) - int32(p[x])
		}
	}
}
