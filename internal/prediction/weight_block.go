package prediction

// 块入口保留具名跨包边界，避免调用方栈上邻域因导入泛型shape的保守逃逸而分配。
// stride以采样计，输入和输出矩形由流水线校验；不保留任何切片。
//
//go:noinline
func BiWeightBlock8(dst []byte, ds int, a, b []byte, ss, w, h int, weights, offsets [2]int32, denom uint32) {
	biWeightBlock(dst, ds, a, b, ss, w, h, weights, offsets, denom, 8)
}

// BiWeightBlock16 保留原生10-bit、负权重和先缩放再平均offset的规范顺序。
//
//go:noinline
func BiWeightBlock16(dst []uint16, ds int, a, b []uint16, ss, w, h int, weights, offsets [2]int32, denom uint32, depth int) {
	biWeightBlock(dst, ds, a, b, ss, w, h, weights, offsets, denom, depth)
}

func biWeightBlock[T sample](dst []T, ds int, a, b []T, ss, w, h int, weights, offsets [2]int32, denom uint32, depth int) {
	// Spec: 8.4.3.2。所有舍入和offset均是块内不变量。
	offset := (((int64(offsets[0]) + int64(offsets[1])) << (depth - 8)) + 1) >> 1
	w0, w1, round := int64(weights[0]), int64(weights[1]), int64(1)<<denom
	if w0 == round && w1 == round && offset == 0 {
		for y := range h {
			d, u, v := dst[y*ds:][:w], a[y*ss:][:w], b[y*ss:][:w]
			for x := range d {
				d[x] = T((int(u[x]) + int(v[x]) + 1) >> 1)
			}
		}
		return
	}
	for y := range h {
		d, u, v := dst[y*ds:][:w], a[y*ss:][:w], b[y*ss:][:w]
		for x := range d {
			d[x] = clipSample[T](int(((int64(u[x])*w0+int64(v[x])*w1+round)>>(denom+1))+offset), depth)
		}
	}
}

// WeightBlock8 将单参考显式权重按块应用，原单采样入口的行为保持不变。
//
//go:noinline
func WeightBlock8(dst []byte, ds int, src []byte, ss, w, h int, weight, offset int32, denom uint32) {
	weightBlock(dst, ds, src, ss, w, h, weight, offset, denom, 8)
}

// WeightBlock16 的offset仍使用码流8-bit标度。
//
//go:noinline
func WeightBlock16(dst []uint16, ds int, src []uint16, ss, w, h int, weight, offset int32, denom uint32, depth int) {
	weightBlock(dst, ds, src, ss, w, h, weight, offset, denom, depth)
}

func weightBlock[T sample](dst []T, ds int, src []T, ss, w, h int, weight, offset int32, denom uint32, depth int) {
	round := int64(0)
	if denom > 0 {
		round = int64(1) << (denom - 1)
	}
	off := int64(offset) << (depth - 8)
	for y := range h {
		d, s := dst[y*ds:][:w], src[y*ss:][:w]
		for x := range d {
			d[x] = clipSample[T](int(((int64(s[x])*int64(weight)+round)>>denom)+off), depth)
		}
	}
}
