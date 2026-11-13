package encoder

import (
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/motion/search"
)

type rdCandidate struct {
	info                   mbInfo
	res                    residual
	pixels                 [384]byte
	vectors                [16]motion.MV
	nz                     [16]uint8
	cnz                    [2][4]uint8
	distortion, rate, cost uint64
}

// analyzeRD 比较 Skip、四种 P 分区、I4、I16 和 PCM。D 为未滤波重建的三平面
// SSE，R 来自真实 CAVLC 宏块写出；mb_skip_run 以独立宏块的最小语法开销估计。
// 候选只修改当前宏块，胜者的像素、模式、NZ 与 MV 一起恢复，避免试探污染邻块。
func (s *frameState) analyzeRD(addr, previousQP, radius int, full bool) (residual, error) {
	base := s.info[addr]
	qp := base.qp
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	reset := func() {
		s.info[addr] = base
		s.clearMotion(addr)
		clear(s.nz[addr*16 : addr*16+16])
		for c := range s.cnz {
			clear(s.cnz[c][addr*4 : addr*4+4])
		}
	}
	// PCM 提供有限上界，所有候选仍用同一个目标 QP 的 lambda。
	reset()
	s.info[addr].pcm = true
	s.mv.SetRect(x, y, 4, 4, motion.MV{}, -1)
	best, err := s.captureRD(addr, previousQP, qp, residual{})
	if err != nil {
		return residual{}, err
	}
	evaluate := func(res residual) error {
		c, err := s.captureRD(addr, previousQP, qp, res)
		if err != nil {
			return err
		}
		if c.cost < best.cost {
			best = c
		}
		return nil
	}
	reset()
	skip := motion.PredictSkip(s.mv, x, y, 0)
	s.mc(addr, partitions[0][0], skip)
	skipRes := s.interResidual(addr)
	if s.info[addr].cbp == 0 {
		s.info[addr].inter = true
		s.info[addr].skip = true
		for i := range s.info[addr].modes {
			s.info[addr].modes[i] = -1
		}
		s.mv.SetRect(x, y, 4, 4, skip, 0)
		if err = evaluate(skipRes); err != nil {
			return residual{}, err
		}
		// 其余非 Skip 宏块最少 5 bit 且 D>=0，此剪枝不会跳过更低 J 的候选。
		if best.cost <= rdCost(qp, 0, 5) {
			s.restoreRD(addr, &best)
			return best.res, nil
		}
	}
	for kind := 0; kind < 4; kind++ {
		reset()
		res := s.interCandidate(addr, kind, radius, full)
		if err = evaluate(res); err != nil {
			return residual{}, err
		}
		// 后续两分区/I16 至少 9 bit；等代价保留先前候选。
		if kind == 0 && best.distortion == 0 && best.rate <= 9 {
			break
		}
	}
	// 若零失真胜者已达到所有剩余帧内候选的最小位数，可安全省去其变换。
	if !(best.distortion == 0 && best.rate <= 9) {
		for _, family := range []int{1, 2} {
			reset()
			res := s.analyzeIntra(addr, family)
			s.mv.SetRect(x, y, 4, 4, motion.MV{}, -1)
			if err = evaluate(res); err != nil {
				return residual{}, err
			}
		}
	}
	s.restoreRD(addr, &best)
	return best.res, nil
}

func (s *frameState) interCandidate(addr, kind, radius int, full bool) residual {
	info := &s.info[addr]
	info.inter = true
	info.partition = kind
	for i := range info.modes {
		info.modes[i] = -1
	}
	x, y := addr%s.pic.MBWidth*16, addr/s.pic.MBWidth*16
	for i, p := range partitions[kind] {
		pred := s.predictPartition(addr, kind, i)
		lambda := motionLambdaQ8(info.qp)
		mv, _ := search.EstimateWithCost(&s.src.Planes[0], &s.reference.Planes[0], x+p.x, y+p.y, p.w, p.h, pred, radius, full, func(m motion.MV, sad int) uint64 {
			return uint64(sad)*256 + lambda*uint64(mvBits(m.X-pred.X, m.Y-pred.Y))
		})
		s.mc(addr, p, mv)
		s.mv.SetRect(x/4+p.x/4, y/4+p.y/4, p.w/4, p.h/4, mv, 0)
	}
	res := s.interResidual(addr)
	s.reconstructInter(addr, &res)
	return res
}

func (s *frameState) captureRD(addr, previousQP, qp int, res residual) (rdCandidate, error) {
	info := &s.info[addr]
	w := bitstream.NewWriter()
	rate := uint64(1)
	if s.reference == nil {
		rate = 0
	}
	var err error
	if info.skip {
		info.qp = previousQP
	} else if info.pcm {
		s.writePCM(w, addr)
		rate += w.BitsWritten()
	} else {
		q := previousQP
		if info.inter {
			err = s.writeInter(w, addr, &q, &res)
		} else {
			err = s.writeMB(w, addr, &q, &res)
		}
		if err != nil {
			return rdCandidate{}, err
		}
		rate += w.BitsWritten()
	}
	c := rdCandidate{info: *info, res: res, rate: rate}
	offset := 0
	for comp := 0; comp < 3; comp++ {
		n := 16
		if comp > 0 {
			n = 8
		}
		x, y := addr%s.pic.MBWidth*n, addr/s.pic.MBWidth*n
		for j := 0; j < n; j++ {
			row := s.pic.Planes[comp].Row(y + j)[x : x+n]
			copy(c.pixels[offset:offset+n], row)
			offset += n
			for i, v := range row {
				d := int64(v) - int64(s.src.Planes[comp].Pix(x+i, y+j))
				c.distortion += uint64(d * d)
			}
		}
	}
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	for r := 0; r < 16; r++ {
		c.vectors[r], _, _ = s.mv.Get(x+r%4, y+r/4)
	}
	copy(c.nz[:], s.nz[addr*16:addr*16+16])
	for comp := range c.cnz {
		copy(c.cnz[comp][:], s.cnz[comp][addr*4:addr*4+4])
	}
	c.cost = rdCost(qp, c.distortion, c.rate)
	return c, nil
}

func (s *frameState) analyzeIntraRD(addr, previousQP int) (residual, error) {
	base := s.info[addr]
	qp := base.qp
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	s.info[addr].pcm = true
	s.mv.SetRect(x, y, 4, 4, motion.MV{}, -1)
	best, err := s.captureRD(addr, previousQP, qp, residual{})
	if err != nil {
		return residual{}, err
	}
	for _, family := range []int{1, 2} {
		s.info[addr] = base
		clear(s.nz[addr*16 : addr*16+16])
		for c := range s.cnz {
			clear(s.cnz[c][addr*4 : addr*4+4])
		}
		res := s.analyzeIntra(addr, family)
		candidate, err := s.captureRD(addr, previousQP, qp, res)
		if err != nil {
			return residual{}, err
		}
		if candidate.cost < best.cost {
			best = candidate
		}
	}
	s.restoreRD(addr, &best)
	return best.res, nil
}

func (s *frameState) restoreRD(addr int, c *rdCandidate) {
	s.info[addr] = c.info
	offset := 0
	for comp := 0; comp < 3; comp++ {
		n := 16
		if comp > 0 {
			n = 8
		}
		x, y := addr%s.pic.MBWidth*n, addr/s.pic.MBWidth*n
		for j := 0; j < n; j++ {
			copy(s.pic.Planes[comp].Row(y + j)[x:x+n], c.pixels[offset:offset+n])
			offset += n
		}
	}
	copy(s.nz[addr*16:addr*16+16], c.nz[:])
	for comp := range c.cnz {
		copy(s.cnz[comp][addr*4:addr*4+4], c.cnz[comp][:])
	}
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	ref := int8(0)
	if !c.info.inter {
		ref = -1
	}
	for r, v := range c.vectors {
		s.mv.SetRect(x+r%4, y+r/4, 1, 1, v, ref)
	}
}
