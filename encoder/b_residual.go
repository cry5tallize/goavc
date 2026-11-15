package encoder

import (
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
)

func (s *frameState) analyzeB(addr, qp int, past, future *picture.Picture) (residual, error) {
	i := &s.info[addr]
	i.inter = true
	for n := range i.modes {
		i.modes[n] = -1
	}
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	s.mv.SetRect(x, y, 4, 4, motion.MV{}, 0)
	for comp := range s.planeCount() {
		mw, mh := s.planeMBSize(comp)
		px, py := addr%s.pic.MBWidth*mw, addr/s.pic.MBWidth*mh
		pl := &s.pic.Planes[comp]
		for yy := range mh {
			for xx := range mw {
				v := prediction.BiWeightSampleHigh(past.Planes[comp].Sample(px+xx, py+yy), future.Planes[comp].Sample(px+xx, py+yy), 1, 1, 0, 0, 0, pl.BitDepth)
				pl.SetSample(px+xx, py+yy, v)
			}
		}
	}
	return s.bResidualRD(addr, qp)
}

// bResidualRD 保留同一direct预测的1/3和1/6死区候选，使用共享SSE+lambda*R
// 选择。R仍是现有CAVLC代理；不声称已实现CABAC精确率估算或B运动搜索。
func (s *frameState) bResidualRD(addr, previousQP int) (residual, error) {
	base := rdCandidate{info: s.info[addr]}
	offset := 0
	for comp := range s.planeCount() {
		mw, mh := s.pic.Format.PlaneDimensions(comp, 16, 16)
		x, y := addr%s.pic.MBWidth*mw, addr/s.pic.MBWidth*mh
		for j := range mh {
			for i := range mw {
				base.pixels[offset+i] = s.pic.Planes[comp].Sample(x+i, y+j)
			}
			offset += mw
		}
	}
	best := rdCandidate{cost: ^uint64(0)}
	for _, inter := range []bool{false, true} {
		s.restoreRD(addr, &base)
		i := &s.info[addr]
		i.interDeadZone = inter
		res := s.interResidual(addr)
		i.skip = i.cbp == 0
		if !i.skip {
			s.reconstructInter(addr, &res)
		}
		candidate, err := s.captureRD(addr, previousQP, base.info.qp, res)
		if err != nil {
			return residual{}, err
		}
		if candidate.cost < best.cost {
			best = candidate
		}
	}
	s.restoreRD(addr, &best)
	// captureRD的CAVLC统计不是已写出的CABAC统计，实际残差从零提交。
	clear(s.nz[addr*16 : addr*16+16])
	for comp := range s.cnz {
		clear(s.cnz[comp][addr*16 : addr*16+16])
	}
	return best.res, nil
}
