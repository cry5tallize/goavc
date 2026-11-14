package filter

import (
	h264 "github.com/goavc/h264"
)

// LumaEdge 对一条亮度边界的 4 像素线段做去块滤波（8.7.2.2/8.7.2.3）。
//
// 参数：
//   - data 是包含 p 侧与 q 侧的完整像素缓冲；
//   - q0 是 q0（边界右侧/下侧第一个像素）在 data 中的下标；
//   - lineStep 是相邻两条线之间的下标差（垂直边界用 stride，水平边界用 1）；
//   - acrossStep 是从 q0 到 p0 的下标差（垂直边界用 1，水平边界用 stride）。
//
// bS 为 0 时不做任何修改；1..3 使用弱滤波，4 使用强滤波。
// qp、alphaDiv2、betaDiv2 由调用方按宏块/切片给出；索引计算遵循
// FFmpeg 的 qp + 52 + 2*div2 约定。
func LumaEdge(data []byte, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2 int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, 4, 8)
}

// LumaEdgeHigh 接收去掉 QpBdOffset 的语法域 QP；所有步长以采样计。
func LumaEdgeHigh(data []uint16, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, bitDepth int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, 4, bitDepth)
}

// LumaEdgeSpan 供MBAFF混合边界按实际帧/场样点分段，仍复用同一滤波算术。
func LumaEdgeSpan(data []byte, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, 8)
}

// LumaEdgeHighSpan 的步长和跨度以原生高位深采样计。
func LumaEdgeHighSpan(data []uint16, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth int) {
	lumaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth)
}

func lumaEdge[T sample](data []T, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth int) {
	if bS <= 0 {
		return
	}
	indexA := qp + 52 + 2*alphaDiv2
	indexB := qp + 52 + 2*betaDiv2
	if indexA < 0 || indexA >= len(alphaTable) || indexB < 0 || indexB >= len(betaTable) {
		return
	}
	scale := 1 << (bitDepth - 8)
	alpha := int(alphaTable[indexA]) * scale
	beta := int(betaTable[indexB]) * scale

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
		if abs(int(data[p0])-int(data[q0i])) >= alpha ||
			abs(int(data[p1])-int(data[p0])) >= beta ||
			abs(int(data[q1])-int(data[q0i])) >= beta {
			continue
		}
		if bS >= 4 {
			strongLumaLine(data, p0, p1, p2, q0i, q1, q2, acrossStep, alpha, beta)
			continue
		}
		weakLumaLine(data, p0, p1, p2, q0i, q1, q2, tc0(bS, indexA)*scale, beta, bitDepth)
	}
}

// weakLumaLine 实现 bS 1..3 的亮度滤波。
//
// p1/q1 的修正只在 |p2-p0|<beta（或 |q2-q0|<beta）时进行，并且
// 使用 tc_orig 作为裁剪范围；p0/q0 使用 tc（可能因 p1/q1 修正而 +1/+2）。
func weakLumaLine[T sample](data []T, p0, p1, p2, q0, q1, q2, tcOrig, beta, bitDepth int) {
	if tcOrig < 0 {
		return
	}
	// 先快照原始像素：规范与 libavcodec 都用 p1/q1 的原始值计算
	// p0/q0 的 delta，即使 p1/q1 已被本次滤波修改。
	p0v, p1v, p2v := int(data[p0]), int(data[p1]), int(data[p2])
	q0v, q1v, q2v := int(data[q0]), int(data[q1]), int(data[q2])
	tc := tcOrig
	if abs(p2v-p0v) < beta {
		if tcOrig != 0 {
			avg := (p2v + ((p0v + q0v + 1) >> 1)) >> 1
			data[p1] = clipPixel[T](p1v+clipInt(avg-p1v, -tcOrig, tcOrig), bitDepth)
		}
		tc++
	}
	if abs(q2v-q0v) < beta {
		if tcOrig != 0 {
			avg := (q2v + ((p0v + q0v + 1) >> 1)) >> 1
			data[q1] = clipPixel[T](q1v+clipInt(avg-q1v, -tcOrig, tcOrig), bitDepth)
		}
		tc++
	}
	delta := clipInt((((q0v-p0v)*4)+p1v-q1v+4)>>3, -tc, tc)
	data[p0] = clipPixel[T](p0v+delta, bitDepth)
	data[q0] = clipPixel[T](q0v-delta, bitDepth)
}

// strongLumaLine 实现 bS==4 的亮度强滤波。
//
// 当 |p0-q0| 较小时使用三抽头/四抽头组合，否则只修正 p0/q0。
// p3/q3 只在对应的 p2/q2 条件成立时读取，因此不会越过图像边界。
func strongLumaLine[T sample](data []T, p0, p1, p2, q0, q1, q2, acrossStep, alpha, beta int) {
	p0v, p1v, p2v := int(data[p0]), int(data[p1]), int(data[p2])
	q0v, q1v, q2v := int(data[q0]), int(data[q1]), int(data[q2])
	// alpha 已按位深缩放；规范的 +2 与各平均式的舍入常量不缩放。
	if abs(p0v-q0v) < (alpha>>2)+2 {
		if abs(p2v-p0v) < beta {
			p3 := p2 - acrossStep
			p3v := 0
			if p3 >= 0 {
				p3v = int(data[p3])
			}
			data[p0] = T((p2v + 2*p1v + 2*p0v + 2*q0v + q1v + 4) >> 3)
			data[p1] = T((p2v + p1v + p0v + q0v + 2) >> 2)
			if p3 >= 0 {
				data[p2] = T((2*p3v + 3*p2v + p1v + p0v + q0v + 4) >> 3)
			}
		} else {
			data[p0] = T((2*p1v + p0v + q1v + 2) >> 2)
		}
		if abs(q2v-q0v) < beta {
			q3 := q2 + acrossStep
			q3v := 0
			if q3 < len(data) {
				q3v = int(data[q3])
			}
			data[q0] = T((p1v + 2*p0v + 2*q0v + 2*q1v + q2v + 4) >> 3)
			data[q1] = T((p0v + q0v + q1v + q2v + 2) >> 2)
			if q3 < len(data) {
				data[q2] = T((2*q3v + 3*q2v + q1v + q0v + p0v + 4) >> 3)
			}
		} else {
			data[q0] = T((2*q1v + q0v + p1v + 2) >> 2)
		}
		return
	}
	data[p0] = T((2*p1v + p0v + q1v + 2) >> 2)
	data[q0] = T((2*q1v + q0v + p1v + 2) >> 2)
}

func tc0(bS, indexA int) int {
	if indexA < 0 || indexA >= len(tc0Table) || bS < 0 || bS > 3 {
		return -1
	}
	return int(tc0Table[indexA][bS])
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clipInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

type sample interface {
	~uint8 | ~uint16
}

func clipPixel[T sample](v, bitDepth int) T {
	if v < 0 {
		return 0
	}
	max := (1 << bitDepth) - 1
	if v > max {
		return T(max)
	}
	return T(v)
}

// ErrInvalidEdge 用于调用方报告越界的边界参数（保留给后续 slice 并行
// 实现做防御性检查；正常路径不会触发）。
var ErrInvalidEdge = h264.ErrInvalidData
