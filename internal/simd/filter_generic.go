package simd

// FilterLuma8 使用调用者准备的阈值。
func FilterLuma8(data []byte, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth int) {
	filterLuma(data, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth, kernels.filterDelta)
}

// FilterLuma16 使用调用者准备的阈值。
func FilterLuma16(data []uint16, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth int) {
	filterLuma(data, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth, kernels.filterDelta)
}
func filterLuma[T filterSample](data []T, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth int, calc func([4]int32, [4]int32) [4]int32) {
	for line := 0; line < lines; line++ {
		q0i := q0 + line*lineStep
		p0 := q0i - acrossStep
		p1 := q0i - 2*acrossStep
		p2 := q0i - 3*acrossStep
		q1 := q0i + acrossStep
		q2 := q0i + 2*acrossStep
		if p2 < 0 || q2 >= len(data) {
			continue
		}
		if filterAbs(int(data[p0])-int(data[q0i])) >= alpha ||
			filterAbs(int(data[p1])-int(data[p0])) >= beta ||
			filterAbs(int(data[q1])-int(data[q0i])) >= beta {
			continue
		}
		if bS >= 4 {
			strongLumaLine(data, p0, p1, p2, q0i, q1, q2, acrossStep, alpha, beta, calc)
			continue
		}
		weakLumaLine(data, p0, p1, p2, q0i, q1, q2, tc, beta, bitDepth, calc)
	}
}

// FilterChroma8 使用调用者准备的阈值。
func FilterChroma8(data []byte, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth int) {
	filterChroma(data, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth, kernels.filterDelta)
}

// FilterChroma16 使用调用者准备的阈值。
func FilterChroma16(data []uint16, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth int) {
	filterChroma(data, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth, kernels.filterDelta)
}
func filterChroma[T filterSample](data []T, q0, lineStep, acrossStep, bS, alpha, beta, tc, lines, bitDepth int, calc func([4]int32, [4]int32) [4]int32) {
	for line := 0; line < lines; line++ {
		q0i := q0 + line*lineStep
		p0 := q0i - acrossStep
		p1 := q0i - 2*acrossStep
		q1 := q0i + acrossStep
		if p1 < 0 || q1 >= len(data) {
			continue
		}
		if filterAbs(int(data[p0])-int(data[q0i])) >= alpha ||
			filterAbs(int(data[p1])-int(data[p0])) >= beta ||
			filterAbs(int(data[q1])-int(data[q0i])) >= beta {
			continue
		}
		p0v, p1v := int(data[p0]), int(data[p1])
		q0v, q1v := int(data[q0i]), int(data[q1])
		if bS >= 4 {
			sums := calc([4]int32{int32(2*p1v + p0v), int32(2*q1v + q0v)}, [4]int32{int32(q1v + 2), int32(p1v + 2)})
			data[p0], data[q0i] = T(sums[0]>>2), T(sums[1]>>2)
			continue
		}
		sums := calc([4]int32{int32((q0v - p0v) * 4)}, [4]int32{int32(p1v - q1v + 4)})
		delta := filterClipInt(int(sums[0]>>3), -tc, tc)
		data[p0] = filterClipPixel[T](int(data[p0])+delta, bitDepth)
		data[q0i] = filterClipPixel[T](int(data[q0i])-delta, bitDepth)
	}
}

// weakLumaLine 实现 bS 1..3 的亮度滤波。
//
// p1/q1 的修正只在 |p2-p0|<beta（或 |q2-q0|<beta）时进行，并且
// 使用 tc_orig 作为裁剪范围；p0/q0 使用 tc（可能因 p1/q1 修正而 +1/+2）。
func weakLumaLine[T filterSample](data []T, p0, p1, p2, q0, q1, q2, tcOrig, beta, bitDepth int, calc func([4]int32, [4]int32) [4]int32) {
	if tcOrig < 0 {
		return
	}
	// 先快照原始像素：规范与 libavcodec 都用 p1/q1 的原始值计算
	// p0/q0 的 delta，即使 p1/q1 已被本次滤波修改。
	p0v, p1v, p2v := int(data[p0]), int(data[p1]), int(data[p2])
	q0v, q1v, q2v := int(data[q0]), int(data[q1]), int(data[q2])
	avg := (p0v + q0v + 1) >> 1
	sums := calc([4]int32{int32((q0v - p0v) * 4), int32(p2v), int32(q2v)}, [4]int32{int32(p1v - q1v + 4), int32(avg), int32(avg)})
	tc := tcOrig
	if filterAbs(p2v-p0v) < beta {
		if tcOrig != 0 {
			avg := int(sums[1] >> 1)
			data[p1] = filterClipPixel[T](p1v+filterClipInt(avg-p1v, -tcOrig, tcOrig), bitDepth)
		}
		tc++
	}
	if filterAbs(q2v-q0v) < beta {
		if tcOrig != 0 {
			avg := int(sums[2] >> 1)
			data[q1] = filterClipPixel[T](q1v+filterClipInt(avg-q1v, -tcOrig, tcOrig), bitDepth)
		}
		tc++
	}
	delta := filterClipInt(int(sums[0]>>3), -tc, tc)
	data[p0] = filterClipPixel[T](p0v+delta, bitDepth)
	data[q0] = filterClipPixel[T](q0v-delta, bitDepth)
}

// strongLumaLine 实现 bS==4 的亮度强滤波。
//
// 当 |p0-q0| 较小时使用三抽头/四抽头组合，否则只修正 p0/q0。
// p3/q3 只在对应的 p2/q2 条件成立时读取，因此不会越过图像边界。
func strongLumaLine[T filterSample](data []T, p0, p1, p2, q0, q1, q2, acrossStep, alpha, beta int, calc func([4]int32, [4]int32) [4]int32) {
	p0v, p1v, p2v := int(data[p0]), int(data[p1]), int(data[p2])
	q0v, q1v, q2v := int(data[q0]), int(data[q1]), int(data[q2])
	sums := calc(
		[4]int32{int32(2*p1v + p0v), int32(2*q1v + q0v), int32(p2v + 2*p1v + 2*p0v), int32(q2v + 2*q1v + 2*q0v)},
		[4]int32{int32(q1v + 2), int32(p1v + 2), int32(2*q0v + q1v + 4), int32(2*p0v + p1v + 4)},
	)
	// alpha 已按位深缩放；规范的 +2 与各平均式的舍入常量不缩放。
	if filterAbs(p0v-q0v) < (alpha>>2)+2 {
		if filterAbs(p2v-p0v) < beta {
			p3 := p2 - acrossStep
			p3v := 0
			if p3 >= 0 {
				p3v = int(data[p3])
			}
			data[p0] = T(sums[2] >> 3)
			data[p1] = T((p2v + p1v + p0v + q0v + 2) >> 2)
			if p3 >= 0 {
				data[p2] = T((2*p3v + 3*p2v + p1v + p0v + q0v + 4) >> 3)
			}
		} else {
			data[p0] = T(sums[0] >> 2)
		}
		if filterAbs(q2v-q0v) < beta {
			q3 := q2 + acrossStep
			q3v := 0
			if q3 < len(data) {
				q3v = int(data[q3])
			}
			data[q0] = T(sums[3] >> 3)
			data[q1] = T((p0v + q0v + q1v + q2v + 2) >> 2)
			if q3 < len(data) {
				data[q2] = T((2*q3v + 3*q2v + q1v + q0v + p0v + 4) >> 3)
			}
		} else {
			data[q0] = T(sums[1] >> 2)
		}
		return
	}
	data[p0] = T(sums[0] >> 2)
	data[q0] = T(sums[1] >> 2)
}

func filterAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func filterClipInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

type filterSample interface {
	~uint8 | ~uint16
}

func filterClipPixel[T filterSample](v, bitDepth int) T {
	if v < 0 {
		return 0
	}
	max := (1 << bitDepth) - 1
	if v > max {
		return T(max)
	}
	return T(v)
}
