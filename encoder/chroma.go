package encoder

import (
	"github.com/cry5tallize/goavc/internal/transform"
)

// 色度块的采样与系数几何只由格式导出；cnz 的宏块步长固定为十六项。
func (s *frameState) planeMBSize(comp int) (int, int) {
	if comp == 0 {
		return 16, 16
	}
	w, h := s.pic.Format.ChromaSubsampling()
	return 16 / w, 16 / h
}

func (s *frameState) planeCount() int {
	if s.separate {
		return 1
	}
	return 3
}

func (s *frameState) chromaArrayType() int {
	if s.separate {
		return 0
	}
	w, h := s.pic.Format.ChromaSubsampling()
	if w == 1 {
		return 3
	}
	if h == 1 {
		return 2
	}
	return 1
}

func (s *frameState) rawMBBits() int {
	n := 256
	if !s.separate {
		w, h := s.planeMBSize(1)
		n += 2 * w * h
	}
	return n * s.pic.Planes[0].BitDepth
}

func (s *frameState) subsampledChromaResidual(addr int, res *residual, intra, reconstructNow bool) {
	info := &s.info[addr]
	w, h := s.planeMBSize(1)
	x, y := addr%s.pic.MBWidth*w, addr/s.pic.MBWidth*h
	qpc := s.chromaQP(info.qp)
	// 块入口确定策略，具名调用使res/coeff/DC保持栈上；不改共享量化公式。
	interDeadZone := !intra && info.interDeadZone
	cbp := 0
	for c := range 2 {
		var dc [8]int32
		for r := range w * h / 16 {
			coeff := residualTransform(&s.src.Planes[c+1], &s.pic.Planes[c+1], x+r%2*4, y+r/2*4)
			dc[r], coeff[0] = coeff[0], 0
			if interDeadZone {
				transform.QuantInter4x4WithScaling(&res.chroma[c][r], &coeff, qpc, s.matrix4(intra, c+1))
			} else {
				transform.Quant4x4WithScaling(&res.chroma[c][r], &coeff, qpc, s.matrix4(intra, c+1))
			}
			if nonzero(res.chroma[c][r][:]) {
				cbp = 2
			}
		}
		if h == 16 {
			var had [8]int32
			// 输入采样与合法 QP 已限界，八点变换不会触及 int32 上限。
			_ = transform.ForwardChromaDC422(&had, &dc)
			if interDeadZone {
				transform.QuantInterDCWithScaling(res.chromaDC[c][:8], had[:], qpc+3, scalingDC(s.matrix4(intra, c+1)))
			} else {
				transform.QuantDCWithScaling(res.chromaDC[c][:8], had[:], qpc+3, scalingDC(s.matrix4(intra, c+1)))
			}
		} else {
			var input, had [4]int32
			copy(input[:], dc[:4])
			transform.ForwardChromaDC(&had, &input)
			if interDeadZone {
				transform.QuantInterDCWithScaling(res.chromaDC[c][:4], had[:], qpc, scalingDC(s.matrix4(intra, c+1)))
			} else {
				transform.QuantDCWithScaling(res.chromaDC[c][:4], had[:], qpc, scalingDC(s.matrix4(intra, c+1)))
			}
		}
		if cbp == 0 && nonzero(res.chromaDC[c][:]) {
			cbp = 1
		}
	}
	info.cbp |= cbp << 4
	if reconstructNow {
		s.reconstructSubsampledChroma(addr, res, intra)
	}
}

func (s *frameState) reconstructSubsampledChroma(addr int, res *residual, intra bool) {
	w, h := s.planeMBSize(1)
	x, y := addr%s.pic.MBWidth*w, addr/s.pic.MBWidth*h
	qpc := s.chromaQP(s.info[addr].qp)
	for c := range 2 {
		var dc [8]int32
		if h == 16 {
			_ = transform.DequantChromaDC422WithScaling(&dc, (*[8]int32)(res.chromaDC[c][:8]), qpc, scalingDC(s.matrix4(intra, c+1)))
		} else {
			var output [4]int32
			transform.DequantChromaDCWithScaling(&output, (*[4]int32)(res.chromaDC[c][:4]), qpc, scalingDC(s.matrix4(intra, c+1)))
			copy(dc[:4], output[:])
		}
		for r := range w * h / 16 {
			reconstructScaled(&s.pic.Planes[c+1], x+r%2*4, y+r/2*4, &res.chroma[c][r], qpc, &dc[r], s.matrix4(intra, c+1))
		}
	}
}

// 444 三平面共享预测模式与宏块 CBP，但独立变换、缩放和非零统计。
// 交换的只是 Plane 描述符，既不复制采样，也不维护第二套编解码内核。
func (s *frameState) chroma444(addr int, res *residual, intra bool) {
	oldPlane, oldSeparate, oldReuse := s.plane, s.separate, s.reuseModes
	defer func() { s.plane, s.separate, s.reuseModes = oldPlane, oldSeparate, oldReuse }()
	for c := 1; c < 3; c++ {
		s.src.Planes[0], s.src.Planes[c] = s.src.Planes[c], s.src.Planes[0]
		s.pic.Planes[0], s.pic.Planes[c] = s.pic.Planes[c], s.pic.Planes[0]
		s.plane, s.separate, s.reuseModes = c, true, true
		var plane residual
		if intra {
			family := 1
			if s.info[addr].is16 {
				family = 2
			} else if s.info[addr].transform8 {
				family = 3
			}
			plane = s.analyzeIntra(addr, family)
		} else {
			plane = s.interResidual(addr)
		}
		res.chroma[c-1], res.chromaDC[c-1], res.chroma8[c-1] = plane.luma, plane.lumaDC, plane.luma8
		s.src.Planes[0], s.src.Planes[c] = s.src.Planes[c], s.src.Planes[0]
		s.pic.Planes[0], s.pic.Planes[c] = s.pic.Planes[c], s.pic.Planes[0]
	}
}

func (s *frameState) reconstructChroma444(addr int, res *residual) {
	oldPlane, oldSeparate := s.plane, s.separate
	defer func() { s.plane, s.separate = oldPlane, oldSeparate }()
	for c := 1; c < 3; c++ {
		s.pic.Planes[0], s.pic.Planes[c] = s.pic.Planes[c], s.pic.Planes[0]
		s.plane, s.separate = c, true
		plane := residual{luma: res.chroma[c-1], lumaDC: res.chromaDC[c-1], luma8: res.chroma8[c-1]}
		s.reconstructInterLuma(addr, &plane)
		s.pic.Planes[0], s.pic.Planes[c] = s.pic.Planes[c], s.pic.Planes[0]
	}
}
