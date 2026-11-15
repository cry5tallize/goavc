package decoder

import (
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
)

// mbaffColocated 将共位图像的物理位置还原为其原生MB/场网格，不能在场视图
// 上直接索引整帧Motion，也不能把顶/底场仅凭相同FrameNum当作同一个参考。
func (fs *frameState) mbaffColocated(s *sliceEntry, mbx, mby, bx, by int) (motion.MV, *dpb.Identity, int8) {
	col := s.refs[1][0]
	root := col
	if col.FrameReference != nil {
		root = col.FrameReference
	}
	grids, references := root.Motion, root.BlockReferences
	fieldFlags := root.MBField
	if fs.sps.SeparateColourPlaneFlag && fs.component > 0 {
		grids, references = root.ChromaMotion[fs.component-1], root.ChromaBlockReferences[fs.component-1]
		fieldFlags = root.ChromaMBField[fs.component-1]
	}
	if fs.sps.Direct8x8InferenceFlag {
		bx = bx / 2 * 3
		by = by / 2 * 3
	}
	addr := fs.mbAddress(mbx, mby)
	row := addr / fs.mbW
	x, y := mbx*16+bx*4, row*16+by*4
	if fs.field {
		y = row/2*32 + fs.parity + 2*by*4
	}
	top := y/32*(2*fs.mbW) + x/16
	colField := top >= 0 && top < len(fieldFlags) && fieldFlags[top]
	colAddr, localY := y/16*fs.mbW+x/16, y%16/4
	structure := motion.FrameMotion
	if colField {
		parity := y & 1
		if !fs.field {
			if absPOC(fs.framePOC, root.FieldPOC[1]) < absPOC(fs.framePOC, root.FieldPOC[0]) {
				parity = 1
			} else {
				parity = 0
			}
		}
		colAddr, localY = top+parity*fs.mbW, y%32/2/4
		structure = motion.TopFieldMotion
		if parity != 0 {
			structure = motion.BottomFieldMotion
		}
	}
	gx, gy := colAddr%fs.mbW*4+x%16/4, colAddr/fs.mbW*4+localY
	for list := range 2 {
		grid := grids[list]
		if grid == nil {
			continue
		}
		mv, index, ok := grid.Get(gx, gy)
		if !ok || index < 0 {
			continue
		}
		i := gy*grid.BlockW + gx
		if i >= len(references[list]) || references[list][i] == nil {
			continue
		}
		id := references[list][i]
		n := motion.NormalizeFieldNeighbor(fs.structure(addr), motion.FieldNeighbor{MV: mv, Ref: index, Structure: structure, Available: true})
		if !fs.field && id.Frame != nil {
			id = id.Frame
		} else if fs.field && id.Frame == nil {
			for _, r := range s.refs[0] {
				if r.ID == id.ID && r.Frame != nil && r.BottomField == (fs.parity != 0) {
					id = r.Identity
					break
				}
			}
		}
		return n.MV, id, n.Ref
	}
	return motion.MV{}, nil, -1
}

func absPOC(a, b int32) int64 {
	d := int64(a) - int64(b)
	if d < 0 {
		return -d
	}
	return d
}
