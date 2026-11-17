package decoder

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/dpb"
	"github.com/cry5tallize/goavc/internal/motion"
)

type directMotion struct {
	refs [2][16]int8
	mvs  [2][16]motion.MV
}

func (fs *frameState) colocated(s *sliceEntry, mbx, mby, bx, by int) (motion.MV, *dpb.Identity, int8) {
	if fs.mbaff {
		return fs.mbaffColocated(s, mbx, mby, bx, by)
	}
	col := s.refs[1][0]
	grids, references := col.Motion, col.BlockReferences
	if fs.sps.SeparateColourPlaneFlag && fs.component > 0 {
		grids, references = col.ChromaMotion[fs.component-1], col.ChromaBlockReferences[fs.component-1]
	}
	if fs.sps.Direct8x8InferenceFlag {
		bx = (bx / 2) * 3
		by = (by / 2) * 3
	}
	x, y := mbx*4+bx, fs.motionY(mby)+by
	for l := 0; l < 2; l++ {
		grid := grids[l]
		if grid == nil {
			continue
		}
		mv, index, ok := grid.Get(x, y)
		if !ok || index < 0 {
			continue
		}
		i := y*grid.BlockW + x
		if i >= len(references[l]) {
			continue
		}
		id := references[l][i]
		if id != nil {
			return mv, id, index
		}
	}
	return motion.MV{}, nil, -1
}

func (fs *frameState) deriveDirect(s *sliceEntry, mbx, mby int) (directMotion, error) {
	var out directMotion
	if len(s.refs[0]) == 0 || len(s.refs[1]) == 0 {
		return out, fmt.Errorf("decoder: direct without both reference lists: %w", h264.ErrInvalidData)
	}
	baseX, baseY := mbx*4, fs.motionY(mby)
	var refs [2]int8
	var pred [2]motion.MV
	if s.hdr.DirectSpatialMVPredFlag {
		for l, grid := range []*motion.Grid{fs.mv, fs.mv1} {
			refs[l] = -1
			var a, b, c int8
			var ao, bo, co bool
			if fs.mbaff {
				addr := fs.mbAddress(mbx, mby)
				n := fs.fieldNeighbors(grid, addr, baseX, baseY, 4)
				if !n.C.Available {
					n.C = n.D
				}
				n.A = motion.NormalizeFieldNeighbor(fs.structure(addr), n.A)
				n.B = motion.NormalizeFieldNeighbor(fs.structure(addr), n.B)
				n.C = motion.NormalizeFieldNeighbor(fs.structure(addr), n.C)
				a, b, c = n.A.Ref, n.B.Ref, n.C.Ref
				ao, bo, co = n.A.Available, n.B.Available, n.C.Available
			} else {
				_, a, ao = grid.Get(baseX-1, baseY)
				_, b, bo = grid.Get(baseX, baseY-1)
				_, c, co = grid.Get(baseX+4, baseY-1)
				if !co {
					_, c, co = grid.Get(baseX-1, baseY-1)
				}
			}
			for _, n := range []struct {
				r  int8
				ok bool
			}{{a, ao}, {b, bo}, {c, co}} {
				if n.ok && n.r >= 0 && (refs[l] < 0 || n.r < refs[l]) {
					refs[l] = n.r
				}
			}
			if refs[l] >= 0 {
				if int(refs[l]) >= len(s.refs[l]) {
					return out, fmt.Errorf("decoder: direct spatial reference exceeds active list: %w", h264.ErrInvalidData)
				}
				if fs.mbaff {
					pred[l] = fs.fieldPrediction(grid, fs.mbAddress(mbx, mby), baseX, baseY, 4, refs[l], motion.FieldMedian, false)
				} else {
					pred[l] = motion.Predict(grid, baseX, baseY, 4, refs[l])
				}
			}
		}
		if refs[0] < 0 && refs[1] < 0 {
			refs = [2]int8{0, 0}
		}
	}
	// 非 MBAFF 的 8×8 推导中，每个象限都查询同一共位块。
	// 首个光栅位置仍完整推导/校验，后续三个位置只重用已验证结果。
	reuse8 := !fs.mbaff && fs.sps.Direct8x8InferenceFlag
	for by := 0; by < 4; by++ {
		for bx := 0; bx < 4; bx++ {
			i := by*4 + bx
			if reuse8 && (bx|by)&1 != 0 {
				first := (by&^1)*4 + (bx &^ 1)
				for l := range 2 {
					out.refs[l][i], out.mvs[l][i] = out.refs[l][first], out.mvs[l][first]
				}
				continue
			}
			colMV, colID, colIndex := fs.colocated(s, mbx, mby, bx, by)
			if s.hdr.DirectSpatialMVPredFlag {
				zero := colID != nil && !s.refs[1][0].LongTerm && colIndex == 0 && absInt(int(colMV.X)) <= 1 && absInt(int(colMV.Y)) <= 1
				for l := 0; l < 2; l++ {
					out.refs[l][i] = refs[l]
					out.mvs[l][i] = pred[l]
					if refs[l] == 0 && zero {
						out.mvs[l][i] = motion.MV{}
					}
				}
			} else {
				out.refs[0][i], out.refs[1][i] = 0, 0
				if colID == nil {
					continue
				}
				mapped := -1
				for n, r := range s.refs[0] {
					if r.ID == colID.ID && (!fs.mbaff || (r.Frame == nil) == (colID.Frame == nil) && (r.Frame == nil || r.BottomField == colID.BottomField)) {
						mapped = n
						break
					}
				}
				if mapped < 0 {
					return out, fmt.Errorf("decoder: temporal direct reference missing from list0: %w", h264.ErrInvalidData)
				}
				out.refs[0][i] = int8(mapped)
				scale := int64(256)
				if !colID.LongTerm {
					scale = int64(motion.DistanceScaleFactor(fs.poc, colID.POC, s.refs[1][0].POC))
				}
				mv := motion.MV{X: int32((scale*int64(colMV.X) + 128) >> 8), Y: int32((scale*int64(colMV.Y) + 128) >> 8)}
				out.mvs[0][i] = mv
				out.mvs[1][i] = motion.MV{X: mv.X - colMV.X, Y: mv.Y - colMV.Y}
			}
			for l := 0; l < 2; l++ {
				if err := out.mvs[l][i].Validate(); err != nil {
					return out, err
				}
			}
		}
	}
	return out, nil
}
