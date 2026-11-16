package simd

// SIMD只接收可证明精确的规范权重域；其他参数由调用方继续原int64公式。
// uint16通过翻转符号位转换为(s-32768)，再把32768*(w0+w1)并入bias，
// 因而完整0..65535输入域均精确，不新增“样本必须<=1023”的旧入口前提。
type blockWeightParameters struct {
	coeff        uint32
	bias, offset int32
	shift        uint32
}

func prepareBlockWeight(weights, offsets [2]int32, denom uint32, depth int, both, wordStorage bool) (p blockWeightParameters, ok bool) {
	if denom > 7 || (depth != 8 && depth != 10) {
		return p, false
	}
	for i := range 2 {
		if weights[i] < -128 || weights[i] > 128 || offsets[i] < -128 || offsets[i] > 127 {
			return p, false
		}
	}
	p.coeff = uint32(uint16(int16(weights[0]))) | uint32(uint16(int16(weights[1])))<<16
	p.shift = denom
	if both {
		p.bias = int32(1) << denom
		p.shift++
		scaled := (int64(offsets[0]) + int64(offsets[1])) << (depth - 8)
		p.offset = int32((scaled + 1) >> 1)
	} else {
		if denom > 0 {
			p.bias = int32(1) << (denom - 1)
		}
		p.offset = offsets[0] << (depth - 8)
	}
	if wordStorage {
		p.bias += 32768 * (weights[0] + weights[1])
	}
	return p, true
}

func vectorSingleBlockOK[T ~uint8 | ~uint16](dst []T, ds int, src []T, ss, w, h int) bool {
	return blockFits(len(dst), ds, w, h) && blockFits(len(src), ss, w, h) && blockIndependent(dst, ds, src, ss, w, h)
}
