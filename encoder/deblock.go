package encoder

import (
	"github.com/goavc/h264/internal/filter"
)

// Spec: 8.7。按帧内、非零系数与运动信息逐段推导 bS。
// 与解码器调用同一 LumaEdge/ChromaEdge 内核；顺序为宏块光栅、先垂直后水平。
// slice 的 idc=0，所以跨 slice 边也滤波；预测在整帧滤波前完成。
func (s *frameState) deblock() {
	mw := s.pic.MBWidth
	for addr, info := range s.info {
		mx, my := addr%mw, addr/mw
		for dir := 0; dir < 2; dir++ {
			for edge := 0; edge < 16; edge += 4 {
				if info.transform8 && edge%8 != 0 {
					continue
				}
				if edge == 0 && (dir == 0 && mx == 0 || dir == 1 && my == 0) {
					continue
				}
				qp, cqp := info.qp, s.chromaQP(info.qp)-6*(s.pic.Planes[0].BitDepth-8)
				if edge == 0 {
					other := addr - 1
					if dir == 1 {
						other = addr - mw
					}
					qp = (qp + s.info[other].qp + 1) >> 1
					cqp = (cqp + s.chromaQP(s.info[other].qp) - 6*(s.pic.Planes[0].BitDepth-8) + 1) >> 1
				}
				for seg := 0; seg < 4; seg++ {
					qx, qy := mx*4+edge/4, my*4+seg
					if dir == 1 {
						qx, qy = mx*4+seg, my*4+edge/4
					}
					px, py := qx-1, qy
					if dir == 1 {
						px, py = qx, qy-1
					}
					bs := s.strength(px, py, qx, qy, edge == 0)
					if bs == 0 {
						continue
					}
					x, y := mx*16+edge, my*16+seg*4
					p := &s.pic.Planes[0]
					along, across := p.Stride, 1
					if dir == 1 {
						x, y = mx*16+seg*4, my*16+edge
						along, across = 1, p.Stride
					}
					if p.BitDepth > 8 {
						filter.LumaEdgeHigh(p.Data16, p.Index(x, y), along, across, bs, qp, 0, 0, p.BitDepth)
					} else {
						filter.LumaEdge(p.Data, p.Index(x, y), along, across, bs, qp, 0, 0)
					}
					if edge%8 != 0 {
						continue
					}
					for c := 1; c < 3; c++ {
						p = &s.pic.Planes[c]
						x, y = mx*8+edge/2, my*8+seg*2
						along, across = p.Stride, 1
						if dir == 1 {
							x, y = mx*8+seg*2, my*8+edge/2
							along, across = 1, p.Stride
						}
						if p.BitDepth > 8 {
							filter.ChromaEdgeHigh(p.Data16, p.Index(x, y), along, across, bs, cqp, 0, 0, 2, p.BitDepth)
						} else {
							filter.ChromaEdge(p.Data, p.Index(x, y), along, across, bs, cqp, 0, 0, 2)
						}
					}
				}
			}
		}
	}
}

// strength 实现 Spec: 8.7.2.1 的单参考 I/P 规则。系数按亮度 4×4 子块计算，
// 同一宏块内部分区可以有不同 MV，不能以宏块整体是否非零代替。
func (s *frameState) strength(px, py, qx, qy int, boundary bool) int {
	mw := s.pic.MBWidth
	pa, qa := py/4*mw+px/4, qy/4*mw+qx/4
	if !s.info[pa].inter || !s.info[qa].inter {
		if boundary {
			return 4
		}
		return 3
	}
	if s.lumaNonzero(pa, px%4, py%4) || s.lumaNonzero(qa, qx%4, qy%4) {
		return 2
	}
	pm, _, _ := s.mv.Get(px, py)
	qm, _, _ := s.mv.Get(qx, qy)
	dx, dy := int(pm.X-qm.X), int(pm.Y-qm.Y)
	if max(dx, -dx) >= 4 || max(dy, -dy) >= 4 {
		return 1
	}
	return 0
}

// CAVLC的四个子流分别保留nC计数；去块却必须按整个8×8变换块判断非零。
// 不能为方便滤波把nz四项强制归一，否则会改变后续CAVLC上下文。
func (s *frameState) lumaNonzero(addr, bx, by int) bool {
	if !s.info[addr].transform8 {
		return s.nz[addr*16+by*4+bx] != 0
	}
	base := addr*16 + (by/2)*8 + (bx/2)*2
	return s.nz[base] != 0 || s.nz[base+1] != 0 || s.nz[base+4] != 0 || s.nz[base+5] != 0
}
