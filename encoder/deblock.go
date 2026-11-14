package encoder

import (
	"github.com/goavc/h264/internal/filter"
)

// Spec: 8.7。按帧内、非零系数与运动信息逐段推导 bS。
// 与解码器调用同一 LumaEdge/ChromaEdge 内核；顺序为宏块光栅、先垂直后水平。
// slice 的 idc=0，所以跨 slice 边也滤波；预测在整帧滤波前完成。
func (s *frameState) deblock() {
	mw := s.pic.MBWidth
	subW, subH := s.pic.Format.ChromaSubsampling()
	for addr, info := range s.info {
		mx, my := addr%mw, addr/mw
		for dir := range 2 {
			for edge := 0; edge < 16; edge += 4 {
				if edge == 0 && (dir == 0 && mx == 0 || dir == 1 && my == 0) {
					continue
				}
				for comp := range s.planeCount() {
					lumaLike := comp == 0 || s.chromaArrayType() == 3
					if lumaLike && info.transform8 && edge%8 != 0 {
						continue
					}
					sx, sy := 1, 1
					if comp > 0 {
						sx, sy = subW, subH
					}
					acrossSub, alongSub := sx, sy
					if dir == 1 {
						acrossSub, alongSub = sy, sx
					}
					// 422水平方向每4行均有色度边；垂直方向每8个亮度采样有一边。
					if edge%(4*acrossSub) != 0 {
						continue
					}
					qp := info.qp
					if comp > 0 {
						qp = s.chromaQP(info.qp) - 6*(s.pic.Planes[comp].BitDepth-8)
					}
					if edge == 0 {
						other := addr - 1
						if dir == 1 {
							other = addr - mw
						}
						otherQP := s.info[other].qp
						if comp > 0 {
							otherQP = s.chromaQP(otherQP) - 6*(s.pic.Planes[comp].BitDepth-8)
						}
						qp = (qp + otherQP + 1) >> 1
					}
					p := &s.pic.Planes[comp]
					for seg := range 4 {
						qx, qy := mx*4+edge/4, my*4+seg
						if dir == 1 {
							qx, qy = mx*4+seg, my*4+edge/4
						}
						px, py := qx-1, qy
						if dir == 1 {
							px, py = qx, qy-1
						}
						// 共同444的三个分量共享由亮度残差推导的bS。
						bs := s.strength(px, py, qx, qy, edge == 0)
						if bs == 0 {
							continue
						}
						x, y := (mx*16+edge)/sx, (my*16+seg*4)/sy
						along, across := p.Stride, 1
						if dir == 1 {
							x, y = (mx*16+seg*4)/sx, (my*16+edge)/sy
							along, across = 1, p.Stride
						}
						if lumaLike {
							if p.BitDepth > 8 {
								filter.LumaEdgeHigh(p.Data16, p.Index(x, y), along, across, bs, qp, 0, 0, p.BitDepth)
							} else {
								filter.LumaEdge(p.Data, p.Index(x, y), along, across, bs, qp, 0, 0)
							}
						} else if p.BitDepth > 8 {
							filter.ChromaEdgeHigh(p.Data16, p.Index(x, y), along, across, bs, qp, 0, 0, 4/alongSub, p.BitDepth)
						} else {
							filter.ChromaEdge(p.Data, p.Index(x, y), along, across, bs, qp, 0, 0, 4/alongSub)
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
	grid := s.nz
	if !s.info[addr].transform8 {
		return grid[addr*16+by*4+bx] != 0
	}
	base := addr*16 + (by/2)*8 + (bx/2)*2
	return grid[base] != 0 || grid[base+1] != 0 || grid[base+4] != 0 || grid[base+5] != 0
}
