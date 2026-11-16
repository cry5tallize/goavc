package prediction

import "github.com/cry5tallize/goavc/internal/simd"

// 块入口保留具名跨包边界，避免调用方栈上邻域因导入泛型shape的保守逃逸而分配。
// stride以采样计，输入和输出矩形由流水线校验；不保留任何切片。
//
//go:noinline
func BiWeightBlock8(dst []byte, ds int, a, b []byte, ss, w, h int, weights, offsets [2]int32, denom uint32) {
	if weightBlockIsAverage(weights, offsets, denom, 8) {
		simd.AverageBlock8(dst, ds, a, b, ss, w, h)
		return
	}
	if w >= 4 && w%4 == 0 && simd.TryBiWeightBlock8(dst, ds, a, b, ss, w, h, weights, offsets, denom) {
		return
	}
	biWeightBlock(dst, ds, a, b, ss, w, h, weights, offsets, denom, 8)
}

// BiWeightBlock16 保留原生10-bit、负权重和先缩放再平均offset的规范顺序。
//
//go:noinline
func BiWeightBlock16(dst []uint16, ds int, a, b []uint16, ss, w, h int, weights, offsets [2]int32, denom uint32, depth int) {
	if weightBlockIsAverage(weights, offsets, denom, depth) {
		simd.AverageBlock16(dst, ds, a, b, ss, w, h)
		return
	}
	if w >= 4 && w%4 == 0 && simd.TryBiWeightBlock16(dst, ds, a, b, ss, w, h, weights, offsets, denom, depth) {
		return
	}
	biWeightBlock(dst, ds, a, b, ss, w, h, weights, offsets, denom, depth)
}

func weightBlockIsAverage(weights, offsets [2]int32, denom uint32, depth int) bool {
	round := int64(1) << denom
	return int64(weights[0]) == round && int64(weights[1]) == round && ((((int64(offsets[0])+int64(offsets[1]))<<(depth-8))+1)>>1) == 0
}

func biWeightBlock[T sample](dst []T, ds int, a, b []T, ss, w, h int, weights, offsets [2]int32, denom uint32, depth int) {
	// Spec: 8.4.3.2。所有舍入和offset均是块内不变量。
	offset := (((int64(offsets[0]) + int64(offsets[1])) << (depth - 8)) + 1) >> 1
	w0, w1, round := int64(weights[0]), int64(weights[1]), int64(1)<<denom
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
	if w >= 4 && w%4 == 0 && simd.TryWeightBlock8(dst, ds, src, ss, w, h, weight, offset, denom) {
		return
	}
	weightBlock(dst, ds, src, ss, w, h, weight, offset, denom, 8)
}

// WeightBlock16 的offset仍使用码流8-bit标度。
//
//go:noinline
func WeightBlock16(dst []uint16, ds int, src []uint16, ss, w, h int, weight, offset int32, denom uint32, depth int) {
	if w >= 4 && w%4 == 0 && simd.TryWeightBlock16(dst, ds, src, ss, w, h, weight, offset, denom, depth) {
		return
	}
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
