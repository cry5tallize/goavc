package decoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/motion"
)

type bPart struct {
	pPartition
	refs [2]int8
	mvs  [2]motion.MV
}
type bGroup struct {
	pPartition
	mask   int
	direct bool
	parts  []bPart
	refs   [2]int8
}

func decodeBMacroblock(fs *frameState, addr, mbx, mby, typ int, s *sliceEntry, qp *int, skip bool) error {
	if fs.mv1 == nil {
		return fmt.Errorf("decoder: missing B motion grid: %w", h264.ErrInvalidData)
	}
	baseX, baseY := mbx*4, fs.motionY(mby)
	var groups []bGroup
	switch {
	case typ == 0:
		groups = []bGroup{{pPartition: pPartition{0, 0, 4, 4}, direct: true}}
		fs.info[addr].direct = true
	case typ <= 3:
		groups = []bGroup{{pPartition: pPartition{0, 0, 4, 4}, mask: typ}}
	case typ <= 21:
		masks := [9][2]int{{1, 1}, {2, 2}, {1, 2}, {2, 1}, {1, 3}, {2, 3}, {3, 1}, {3, 2}, {3, 3}}
		pair := masks[(typ-4)/2]
		for i := 0; i < 2; i++ {
			p := pPartition{0, i * 2, 4, 2}
			if typ%2 == 1 {
				p = pPartition{i * 2, 0, 2, 4}
			}
			groups = append(groups, bGroup{pPartition: p, mask: pair[i]})
		}
	case typ == 22:
		for i := 0; i < 4; i++ {
			v, err := s.readBSubType()
			if err != nil {
				return err
			}
			if v < 0 || v > 12 {
				return fmt.Errorf("decoder: B sub_mb_type %d: %w", v, h264.ErrInvalidData)
			}
			x, y := (i%2)*2, (i/2)*2
			g := bGroup{pPartition: pPartition{x, y, 2, 2}, direct: v == 0}
			w, h := 2, 2
			switch v {
			case 1, 4, 5, 10:
				g.mask = 1
			case 2, 6, 7, 11:
				g.mask = 2
			case 3, 8, 9, 12:
				g.mask = 3
			}
			if v >= 10 {
				w, h = 1, 1
			} else if v >= 4 {
				if v%2 == 0 {
					h = 1
				} else {
					w = 1
				}
			}
			for yy := y; yy < y+2; yy += h {
				for xx := x; xx < x+2; xx += w {
					g.parts = append(g.parts, bPart{pPartition: pPartition{xx, yy, w, h}})
				}
			}
			groups = append(groups, g)
		}
	default:
		return fmt.Errorf("decoder: B macroblock type %d: %w", typ, h264.ErrInvalidData)
	}
	needDirect := false
	fs.info[addr].allow8 = true
	for _, g := range groups {
		if g.direct {
			needDirect = true
			if !fs.sps.Direct8x8InferenceFlag {
				fs.info[addr].allow8 = false
			}
		} else {
			for _, p := range g.parts {
				if p.w < 2 || p.h < 2 {
					fs.info[addr].allow8 = false
				}
			}
		}
	}
	var direct directMotion
	if needDirect {
		var err error
		direct, err = fs.deriveDirect(s, mbx, mby)
		if err != nil {
			return err
		}
	}
	for i := range groups {
		g := &groups[i]
		g.refs = [2]int8{-1, -1}
		if g.direct {
			g.parts = nil
			for yy := g.y; yy < g.y+g.h; yy++ {
				for xx := g.x; xx < g.x+g.w; xx++ {
					r := yy*4 + xx
					p := bPart{pPartition: pPartition{xx, yy, 1, 1}}
					for l := 0; l < 2; l++ {
						p.refs[l] = direct.refs[l][r]
						p.mvs[l] = direct.mvs[l][r]
					}
					g.parts = append(g.parts, p)
					fs.directBlocks[(baseY+yy)*fs.mv.BlockW+baseX+xx] = true
				}
			}
		} else if len(g.parts) == 0 {
			g.parts = []bPart{{pPartition: g.pPartition}}
		}
	}
	// 参考索引按 list 外层、分区内层读取，随后 MVD 也遵守该顺序。
	for l, grid := range []*motion.Grid{fs.mv, fs.mv1} {
		for i := range groups {
			g := &groups[i]
			if g.direct {
				for _, p := range g.parts {
					grid.Ref[(baseY+p.y)*grid.BlockW+baseX+p.x] = p.refs[l]
				}
				continue
			}
			if g.mask&(1<<l) != 0 {
				r, err := s.readReferenceIndex(fs, addr, baseX+g.x, baseY+g.y, l)
				if err != nil {
					return err
				}
				g.refs[l] = r
			}
			for yy := baseY + g.y; yy < baseY+g.y+g.h; yy++ {
				for xx := baseX + g.x; xx < baseX+g.x+g.w; xx++ {
					grid.Ref[yy*grid.BlockW+xx] = g.refs[l]
				}
			}
			for p := range g.parts {
				g.parts[p].refs = g.refs
			}
		}
	}
	for l, grid := range []*motion.Grid{fs.mv, fs.mv1} {
		for gi := range groups {
			g := &groups[gi]
			for i := range g.parts {
				p := &g.parts[i]
				x, y := baseX+p.x, baseY+p.y
				r := p.refs[l]
				if !g.direct && r >= 0 {
					var pred motion.MV
					if !fs.mbaff {
						if typ >= 4 && typ <= 21 {
							if typ%2 == 0 {
								pred = motion.Predict16x8(grid, x, y, gi == 1, r)
							} else {
								pred = motion.Predict8x16(grid, x, y, gi == 1, r)
							}
						} else {
							pred = motion.Predict(grid, x, y, p.w, r)
						}
					}
					if fs.mbaff {
						partition := motion.FieldMedian
						if typ >= 4 && typ <= 21 {
							if typ%2 == 0 {
								partition = motion.Field16x8Top
								if gi == 1 {
									partition = motion.Field16x8Bottom
								}
							} else {
								partition = motion.Field8x16Left
								if gi == 1 {
									partition = motion.Field8x16Right
								}
							}
						}
						pred = fs.fieldPrediction(grid, addr, x, y, p.w, r, partition, false)
					}
					var diff motion.MV
					var err error
					if s.cabac != nil {
						diff.X, err = s.cabac.readMVDForList(fs, x, y, 0, l)
					} else {
						diff.X, err = s.data.ReadSE()
					}
					if err != nil {
						return err
					}
					if s.cabac != nil {
						diff.Y, err = s.cabac.readMVDForList(fs, x, y, 1, l)
					} else {
						diff.Y, err = s.data.ReadSE()
					}
					if err != nil {
						return err
					}
					mx, my := int64(pred.X)+int64(diff.X), int64(pred.Y)+int64(diff.Y)
					if mx < -motion.MaxAbsMV || mx > motion.MaxAbsMV || my < -motion.MaxAbsMV || my > motion.MaxAbsMV {
						return fmt.Errorf("decoder: B MVD overflow: %w", h264.ErrInvalidData)
					}
					p.mvs[l] = motion.MV{X: int32(mx), Y: int32(my)}
					if s.cabac != nil {
						data := fs.mvd
						if l == 1 {
							data = fs.mvd1
						}
						for yy := y; yy < y+p.h; yy++ {
							for xx := x; xx < x+p.w; xx++ {
								data[yy*grid.BlockW+xx] = diff
							}
						}
					}
				}
				grid.SetRect(x, y, p.w, p.h, p.mvs[l], r)
				if r >= 0 {
					fs.setReferenceIdentity(l, x, y, p.w, p.h, s.refs[l][r].Identity)
				}
			}
		}
	}
	for _, g := range groups {
		for _, p := range g.parts {
			fs.mcBPartition(s, mbx, mby, p.pPartition, p.refs, p.mvs)
		}
	}
	if skip {
		return nil
	}
	return fs.interResidual(s, addr, mbx, mby, qp)
}
