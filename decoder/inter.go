package decoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/motion"
)

// pPartition 坐标与尺寸均以 4×4 块为单位。参考索引属于宏块分区或
// 8×8 子宏块，MVD 则属于每个子分区，二者的读取顺序不能交错。
type pPartition struct{ x, y, w, h int }
type pGroup struct {
	pPartition
	parts []pPartition
	ref   int8
}

func decodeInterMacroblock(fs *frameState, addr, mbx, mby, pType int, s *sliceEntry, qscale *int) error {
	if len(s.refs[0]) == 0 {
		return fmt.Errorf("decoder: P macroblock without reference list: %w", h264.ErrInvalidData)
	}
	baseX, baseY := mbx*4, mby*4
	var groups []pGroup
	switch pType {
	case 0:
		groups = []pGroup{{pPartition: pPartition{0, 0, 4, 4}}}
	case 1:
		groups = []pGroup{{pPartition: pPartition{0, 0, 4, 2}}, {pPartition: pPartition{0, 2, 4, 2}}}
	case 2:
		groups = []pGroup{{pPartition: pPartition{0, 0, 2, 4}}, {pPartition: pPartition{2, 0, 2, 4}}}
	case 3, 4:
		var types [4]uint32
		for i := range types {
			v, err := s.readPSubType()
			if err != nil {
				return err
			}
			if v > 3 {
				return fmt.Errorf("decoder: P sub_mb_type %d: %w", v, h264.ErrInvalidData)
			}
			types[i] = v
		}
		for i, typ := range types {
			x, y := (i%2)*2, (i/2)*2
			g := pGroup{pPartition: pPartition{x, y, 2, 2}}
			switch typ {
			case 0:
				g.parts = []pPartition{{x, y, 2, 2}}
			case 1:
				g.parts = []pPartition{{x, y, 2, 1}, {x, y + 1, 2, 1}}
			case 2:
				g.parts = []pPartition{{x, y, 1, 2}, {x + 1, y, 1, 2}}
			case 3:
				g.parts = []pPartition{{x, y, 1, 1}, {x + 1, y, 1, 1}, {x, y + 1, 1, 1}, {x + 1, y + 1, 1, 1}}
			}
			groups = append(groups, g)
		}
	default:
		return fmt.Errorf("decoder: P mb_type %d: %w", pType, h264.ErrInvalidData)
	}
	for i := range groups {
		g := &groups[i]
		if pType != 4 {
			index, err := s.readReferenceIndex(fs, addr, baseX+g.x, baseY+g.y)
			if err != nil {
				return err
			}
			g.ref = index
		}
		for y := baseY + g.y; y < baseY+g.y+g.h; y++ {
			for x := baseX + g.x; x < baseX+g.x+g.w; x++ {
				fs.mv.Ref[y*fs.mv.BlockW+x] = g.ref
			}
		}
		if len(g.parts) == 0 {
			g.parts = []pPartition{g.pPartition}
		}
	}
	fs.info[addr].allow8 = true
	for _, g := range groups {
		for _, p := range g.parts {
			if p.w < 2 || p.h < 2 {
				fs.info[addr].allow8 = false
			}
		}
	}
	for gi, g := range groups {
		for _, p := range g.parts {
			x, y := baseX+p.x, baseY+p.y
			pred := motion.Predict(fs.mv, x, y, p.w, g.ref)
			if pType == 1 {
				pred = motion.Predict16x8(fs.mv, x, y, gi == 1, g.ref)
			} else if pType == 2 {
				pred = motion.Predict8x16(fs.mv, x, y, gi == 1, g.ref)
			}
			var diff motion.MV
			var err error
			if s.cabac != nil {
				diff.X, err = s.cabac.readMVDComponent(fs, x, y, 0)
			} else {
				diff.X, err = s.data.ReadSE()
			}
			if err != nil {
				return err
			}
			if s.cabac != nil {
				diff.Y, err = s.cabac.readMVDComponent(fs, x, y, 1)
			} else {
				diff.Y, err = s.data.ReadSE()
			}
			if err != nil {
				return err
			}
			mx, my := int64(pred.X)+int64(diff.X), int64(pred.Y)+int64(diff.Y)
			if mx < -motion.MaxAbsMV || mx > motion.MaxAbsMV || my < -motion.MaxAbsMV || my > motion.MaxAbsMV {
				return fmt.Errorf("decoder: P motion vector overflow: %w", h264.ErrInvalidData)
			}
			mv := motion.MV{X: int32(mx), Y: int32(my)}
			fs.mv.SetRect(x, y, p.w, p.h, mv, g.ref)
			if s.cabac != nil {
				for yy := y; yy < y+p.h; yy++ {
					for xx := x; xx < x+p.w; xx++ {
						fs.mvd[yy*fs.mv.BlockW+xx] = diff
					}
				}
			}
			r := s.refs[0][g.ref]
			fs.setReferenceIdentity(0, x, y, p.w, p.h, r.Identity)
			fs.mcPartition(r.Picture, mbx, mby, p.x*4, p.y*4, p.w*4, p.h*4, mv)
			s.applyPWeights(fs, mbx, mby, p.x*4, p.y*4, p.w*4, p.h*4, int(g.ref))
		}
	}
	return fs.interResidual(s, addr, mbx, mby, qscale)
}

func (fs *frameState) interResidual(s *sliceEntry, addr, mbx, mby int, qscale *int) error {
	cbp, err := s.readCBP(fs, addr, false)
	if err != nil {
		return err
	}
	fs.info[addr].cbp = cbp
	if s.pps.Transform8x8ModeFlag && cbp&15 != 0 && fs.info[addr].allow8 {
		v, err := s.readTransformFlag(fs, addr)
		if err != nil {
			return err
		}
		fs.info[addr].transform8 = v
	}
	if cbp != 0 {
		if err := s.updateQP(qscale); err != nil {
			return err
		}
		fs.info[addr].qp = *qscale
	}
	var res residual
	if err := fs.decodeResiduals(s, addr, mbx, mby, false, false, cbp, &res); err != nil {
		return err
	}
	if err := fs.addLumaResidual(mbx, mby, cbp, false, *qscale, &res); err != nil {
		return err
	}
	return fs.addChromaResidual(mbx, mby, (cbp>>4)&3, *qscale, &res)
}
