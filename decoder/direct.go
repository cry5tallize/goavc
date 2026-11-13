package decoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/dpb"
	"github.com/goavc/h264/internal/motion"
)

type directMotion struct {
	refs [2][16]int8
	mvs  [2][16]motion.MV
}

func (fs *frameState) colocated(s *sliceEntry, mbx, mby, bx, by int) (motion.MV, *dpb.Identity, int8) {
	col := s.refs[1][0]
	if fs.sps.Direct8x8InferenceFlag {
		bx = (bx / 2) * 3
		by = (by / 2) * 3
	}
	x, y := mbx*4+bx, mby*4+by
	for l := 0; l < 2; l++ {
		grid := col.Motion[l]
		if grid == nil {
			continue
		}
		mv, index, ok := grid.Get(x, y)
		if !ok || index < 0 {
			continue
		}
		i := y*grid.BlockW + x
		if i >= len(col.BlockReferences[l]) {
			continue
		}
		id := col.BlockReferences[l][i]
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
	baseX, baseY := mbx*4, mby*4
	var refs [2]int8
	var pred [2]motion.MV
	if s.hdr.DirectSpatialMVPredFlag {
		for l, grid := range []*motion.Grid{fs.mv, fs.mv1} {
			refs[l] = -1
			_, a, ao := grid.Get(baseX-1, baseY)
			_, b, bo := grid.Get(baseX, baseY-1)
			_, c, co := grid.Get(baseX+4, baseY-1)
			if !co {
				_, c, co = grid.Get(baseX-1, baseY-1)
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
				pred[l] = motion.Predict(grid, baseX, baseY, 4, refs[l])
			}
		}
		if refs[0] < 0 && refs[1] < 0 {
			refs = [2]int8{0, 0}
		}
	}
	for by := 0; by < 4; by++ {
		for bx := 0; bx < 4; bx++ {
			i := by*4 + bx
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
					if r.ID == colID.ID {
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
