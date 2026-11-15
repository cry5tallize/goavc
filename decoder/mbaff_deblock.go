package decoder

import (
	"github.com/cry5tallize/goavc/internal/filter"
	"github.com/cry5tallize/goavc/internal/transform"
)

// MBAFF去块按宏块对地址顺序。混合边界逐采样定位p/q所属MB，再调用同一滤波核；
// 不能把两个不同场的p行当成普通相邻帧行，或按光栅行改变滤波依赖顺序。
func (fs *frameState) deblockMBAFF() error {
	first := fs.component
	defer func() { fs.component = first; fs.restoreFrame() }()
	fs.restoreFrame()
	for comp := 0; comp < fs.fullPlaneCount(); comp++ {
		fs.component = first + comp
		for scan := 0; scan < fs.total; scan++ {
			addr := fs.rasterMB(scan)
			if fs.info[addr].deblockIDC == 1 {
				continue
			}
			x, y := addr%fs.mbW, addr/fs.mbW
			if !fs.fieldAt(addr) && (x == 0 || !fs.fieldAt(addr-1)) && (y == 0 || !fs.fieldAt(addr-fs.mbW)) {
				if err := fs.deblockMB(x, y); err != nil {
					return err
				}
				continue
			}
			for _, vertical := range []bool{true, false} {
				for edge := 0; edge < 16; edge += 4 {
					if !fs.info[addr].transform8 || edge%8 == 0 {
						fs.filterMBAFFEdge(addr, fs.component, edge, vertical, true)
					}
					if fs.sps.ChromaFormatIDC != 3 {
						sub := fs.subH
						if vertical {
							sub = fs.subW
						}
						if edge%(4*sub) == 0 {
							for c := 1; c < 3; c++ {
								fs.filterMBAFFEdge(addr, c, edge/sub, vertical, false)
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func (fs *frameState) mbaFFStrength(pAddr, pbx, pby, qAddr, qbx, qby int, boundary, horizontal bool) int {
	p, q := &fs.info[pAddr], &fs.info[qAddr]
	if p.intra || q.intra || p.pcm || q.pcm {
		if !boundary || horizontal && (fs.fieldAt(pAddr) || fs.fieldAt(qAddr)) {
			return 3
		}
		return 4
	}
	if fs.lumaNonzero(pAddr, pbx, pby) || fs.lumaNonzero(qAddr, qbx, qby) {
		return 2
	}
	if fs.fieldAt(pAddr) != fs.fieldAt(qAddr) {
		return 1
	}
	px, py := pAddr%fs.mbW*4+pbx, pAddr/fs.mbW*4+pby
	qx, qy := qAddr%fs.mbW*4+qbx, qAddr/fs.mbW*4+qby
	limit := 4
	if fs.fieldAt(qAddr) {
		limit = 2
	}
	if fs.mv1 != nil {
		return fs.bMotionStrengthLimit(px, py, qx, qy, limit)
	}
	pmv, pref, pok := fs.mv.Get(px, py)
	qmv, qref, qok := fs.mv.Get(qx, qy)
	same := pref == qref
	if fs.referenceIDs[0] != nil {
		same = fs.referenceIDs[0][py*fs.mv.BlockW+px] == fs.referenceIDs[0][qy*fs.mv.BlockW+qx]
	}
	if !pok || !qok || !same || absInt(int(pmv.X-qmv.X)) >= 4 || absInt(int(pmv.Y-qmv.Y)) >= limit {
		return 1
	}
	return 0
}

func (fs *frameState) mbaFFQP(addr, comp int) int {
	qp := fs.info[addr].qp
	if comp == 0 || fs.sps.SeparateColourPlaneFlag {
		return qp
	}
	offset := int(fs.pps.ChromaQPIndexOffset)
	if comp == 2 {
		offset = int(fs.pps.SecondChromaQPIndexOffset)
	}
	qpc, _ := transform.ScaleChromaQP(qp, offset, 8+int(fs.sps.BitDepthChromaMinus8))
	return qpc - 6*int(fs.sps.BitDepthChromaMinus8)
}

func (fs *frameState) filterMBAFFEdge(addr, comp, edge int, vertical, lumaKernel bool) {
	w, h := 16, 16
	if comp > 0 {
		w, h = fs.chromaW, fs.chromaH
	}
	pl := &fs.framePic.Planes[comp]
	mbx, row := addr%fs.mbW, addr/fs.mbW
	xBase, yBase := mbx*w, row*h
	step := 1
	if fs.fieldAt(addr) {
		yBase = row/2*(2*h) + (row & 1)
		step = 2
	}
	passes := 1
	across := 1
	if !vertical {
		across = step * pl.Stride
		if edge == 0 && !fs.fieldAt(addr) && yBase > 0 {
			other, _, _ := fs.locatePhysical(comp, xBase, yBase-1)
			if fs.fieldAt(other) {
				passes = 2
				across = 2 * pl.Stride
			}
		}
	}
	for pass := 0; pass < passes; pass++ {
		lines := w
		if vertical {
			lines = h
		}
		for i := 0; i < lines; i++ {
			x, y := xBase+i, yBase+step*edge+pass
			qx, qy := i, edge+pass
			px, py := x, y-across/pl.Stride
			if vertical {
				x, y = xBase+edge, yBase+step*i
				qx, qy = edge, i
				px, py = x-1, y
			}
			pAddr, pX, pY := fs.locatePhysical(comp, px, py)
			if pAddr < 0 {
				continue
			}
			q := &fs.info[addr]
			if q.deblockIDC == 2 && fs.info[pAddr].sliceID != q.sliceID {
				continue
			}
			sx, sy := 1, 1
			if comp > 0 {
				sx, sy = fs.subW, fs.subH
			}
			bs := fs.mbaFFStrength(pAddr, pX*sx/4, pY*sy/4, addr, qx*sx/4, qy*sy/4, edge == 0, !vertical)
			if bs == 0 {
				continue
			}
			qp := fs.mbaFFQP(addr, comp)
			if edge == 0 {
				qp = (qp + fs.mbaFFQP(pAddr, comp) + 1) >> 1
			}
			q0 := pl.Index(x, y)
			if lumaKernel {
				if pl.BitDepth == 10 {
					filter.LumaEdgeHighSpan(pl.Data16, q0, 1, across, bs, qp, int(q.alphaDiv2), int(q.betaDiv2), 1, pl.BitDepth)
				} else {
					filter.LumaEdgeSpan(pl.Data, q0, 1, across, bs, qp, int(q.alphaDiv2), int(q.betaDiv2), 1)
				}
			} else {
				if pl.BitDepth == 10 {
					filter.ChromaEdgeHigh(pl.Data16, q0, 1, across, bs, qp, int(q.alphaDiv2), int(q.betaDiv2), 1, pl.BitDepth)
				} else {
					filter.ChromaEdge(pl.Data, q0, 1, across, bs, qp, int(q.alphaDiv2), int(q.betaDiv2), 1)
				}
			}
		}
	}
}
