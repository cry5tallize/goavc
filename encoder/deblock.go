package encoder

import (
	"github.com/goavc/h264/internal/filter"
	"github.com/goavc/h264/internal/transform"
)

// Spec: 8.7。M4 全部是 intra，宏块外边 bS=4、内部 bS=3。
// 与解码器调用同一 LumaEdge/ChromaEdge 内核；顺序为宏块光栅、先垂直后水平。
// slice 的 idc=0，所以跨 slice 边也滤波；预测在整帧滤波前完成。
func (s *frameState) deblock() {
	mw := s.pic.MBWidth
	for addr, info := range s.info {
		mx, my := addr%mw, addr/mw
		for dir := 0; dir < 2; dir++ {
			for edge := 0; edge < 16; edge += 4 {
				if edge == 0 && (dir == 0 && mx == 0 || dir == 1 && my == 0) {
					continue
				}
				bs, qp, cqp := 3, info.qp, transform.ChromaQP(info.qp, 0)
				if edge == 0 {
					bs = 4
					other := addr - 1
					if dir == 1 {
						other = addr - mw
					}
					qp = (qp + s.info[other].qp + 1) >> 1
					cqp = (cqp + transform.ChromaQP(s.info[other].qp, 0) + 1) >> 1
				}
				for seg := 0; seg < 4; seg++ {
					x, y := mx*16+edge, my*16+seg*4
					p := &s.pic.Planes[0]
					along, across := p.Stride, 1
					if dir == 1 {
						x, y = mx*16+seg*4, my*16+edge
						along, across = 1, p.Stride
					}
					filter.LumaEdge(p.Data, p.Index(x, y), along, across, bs, qp, 0, 0)
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
						filter.ChromaEdge(p.Data, p.Index(x, y), along, across, bs, cqp, 0, 0, 2)
					}
				}
			}
		}
	}
}
