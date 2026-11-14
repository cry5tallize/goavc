package filter

// ChromaEdge 对一条色度边界的像素线段做去块滤波。
//
// 4:2:0 下每条边界有 8 行/列，FFmpeg 把每个 bS 应用到 2 条线，
// 因此调用方以 lines=2 逐段调用。bS==4 使用色度强滤波，否则使用
// 带 +1 修正的弱滤波（规范 8.7.2.4）。
func ChromaEdge(data []byte, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines int) {
	chromaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, 8)
}

// ChromaEdgeHigh 的 QP 为语法域色度 QP，步长以采样计。
func ChromaEdgeHigh(data []uint16, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth int) {
	chromaEdge(data, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth)
}

func chromaEdge[T sample](data []T, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines, bitDepth int) {
	if bS <= 0 || lines <= 0 {
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
	tc := 0
	if bS < 4 {
		// tc0 按位深缩放，但规范色度修正项 +1 不缩放。
		tc = tc0(bS, indexA)*scale + 1
		if tc <= 0 {
			return
		}
	}
	for line := 0; line < lines; line++ {
		q0i := q0 + line*lineStep
		p0 := q0i - acrossStep
		p1 := q0i - 2*acrossStep
		q1 := q0i + acrossStep
		if p1 < 0 || q1 >= len(data) {
			continue
		}
		if abs(int(data[p0])-int(data[q0i])) >= alpha ||
			abs(int(data[p1])-int(data[p0])) >= beta ||
			abs(int(data[q1])-int(data[q0i])) >= beta {
			continue
		}
		if bS >= 4 {
			p0v := int(data[p0])
			p1v := int(data[p1])
			q0v := int(data[q0i])
			q1v := int(data[q1])
			data[p0] = T((2*p1v + p0v + q1v + 2) >> 2)
			data[q0i] = T((2*q1v + q0v + p1v + 2) >> 2)
			continue
		}
		delta := clipInt((((int(data[q0i])-int(data[p0]))*4)+int(data[p1])-int(data[q1])+4)>>3, -tc, tc)
		data[p0] = clipPixel[T](int(data[p0])+delta, bitDepth)
		data[q0i] = clipPixel[T](int(data[q0i])-delta, bitDepth)
	}
}
