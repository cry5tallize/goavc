package filter

// ChromaEdge 对一条色度边界的像素线段做去块滤波。
//
// 4:2:0 下每条边界有 8 行/列，FFmpeg 把每个 bS 应用到 2 条线，
// 因此调用方以 lines=2 逐段调用。bS==4 使用色度强滤波，否则使用
// 带 +1 修正的弱滤波（规范 8.7.2.4）。
func ChromaEdge(data []byte, q0, lineStep, acrossStep, bS, qp, alphaDiv2, betaDiv2, lines int) {
	if bS <= 0 || lines <= 0 {
		return
	}
	indexA := qp + 52 + 2*alphaDiv2
	indexB := qp + 52 + 2*betaDiv2
	if indexA < 0 || indexA >= len(alphaTable) || indexB < 0 || indexB >= len(betaTable) {
		return
	}
	alpha := int(alphaTable[indexA])
	beta := int(betaTable[indexB])
	tc := 0
	if bS < 4 {
		tc = tc0(bS, indexA) + 1
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
			data[p0] = byte((2*p1v + p0v + q1v + 2) >> 2)
			data[q0i] = byte((2*q1v + q0v + p1v + 2) >> 2)
			continue
		}
		delta := clipInt((((int(data[q0i])-int(data[p0]))*4)+int(data[p1])-int(data[q1])+4)>>3, -tc, tc)
		data[p0] = clipPixel(int(data[p0]) + delta)
		data[q0i] = clipPixel(int(data[q0i]) - delta)
	}
}
