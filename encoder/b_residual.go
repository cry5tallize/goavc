package encoder

// bResidualRD 保留同一direct预测的1/3和1/6死区候选，使用共享SSE+lambda*R
// 选择。R仍是现有CAVLC代理；不声称已实现CABAC精确率估算或B运动搜索。
func (s *frameState) bResidualRD(addr, previousQP int) (residual, error) {
	base := rdCandidate{info: s.info[addr]}
	offset := 0
	for comp := 0; comp < 3; comp++ {
		n := 16
		if comp > 0 {
			n = 8
		}
		x, y := addr%s.pic.MBWidth*n, addr/s.pic.MBWidth*n
		for j := 0; j < n; j++ {
			for i := range n {
				base.pixels[offset+i] = s.pic.Planes[comp].Sample(x+i, y+j)
			}
			offset += n
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
		clear(s.cnz[comp][addr*4 : addr*4+4])
	}
	return best.res, nil
}
