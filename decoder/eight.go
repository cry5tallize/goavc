package decoder

import (
	"github.com/goavc/h264/internal/prediction"
	"github.com/goavc/h264/internal/transform"
)

func (s *sliceEntry) readTransformFlag(fs *frameState, addr int) (bool, error) {
	if s.cabac == nil {
		v, err := s.data.ReadBits(1)
		return v != 0, err
	}
	ctx := 0
	for side, other := range []int{addr - 1, addr - fs.mbW} {
		if side == 0 && addr%fs.mbW == 0 {
			continue
		}
		if fs.neighborAvailable(addr, other) && fs.info[other].transform8 {
			ctx++
		}
	}
	return s.cabac.bin(399+ctx) != 0, s.cabac.d.Err()
}

func (fs *frameState) matrix4(intra bool, component int) *[16]uint8 {
	if !fs.sps.SeqScalingMatrixPresentFlag && !fs.pps.PicScalingMatrixPresentFlag {
		return nil
	}
	i := component
	if !intra {
		i += 3
	}
	return &fs.scaling.Four[i]
}

func (fs *frameState) matrix8(intra bool) *[64]uint8 {
	if !fs.sps.SeqScalingMatrixPresentFlag && !fs.pps.PicScalingMatrixPresentFlag {
		return nil
	}
	i := 1
	if intra {
		i = 0
	}
	return &fs.scaling.Eight[i]
}

func dcScale(matrix *[16]uint8) uint8 {
	if matrix == nil {
		return 16
	}
	return matrix[0]
}

func (fs *frameState) intra8Samples(mbx, mby, bx, by int) (prediction.Intra8x8Samples, bool, bool) {
	var out prediction.Intra8x8Samples
	pl := &fs.pic.Planes[0]
	addr := mby*fs.mbW + mbx
	x, y := mbx*16+bx*8, mby*16+by*8
	top := by > 0 || fs.intraNeighbor(addr, addr-fs.mbW)
	left := bx > 0 || (mbx > 0 && fs.intraNeighbor(addr, addr-1))
	tr := false
	if by == 0 {
		tr = bx == 0 || (mbx+1 < fs.mbW && fs.intraNeighbor(addr, addr-fs.mbW+1))
	} else {
		tr = bx == 0
	}
	tl := false
	switch {
	case bx > 0 && by > 0:
		tl = true
	case bx > 0:
		tl = top
	case by > 0:
		tl = left
	default:
		tl = mbx > 0 && fs.intraNeighbor(addr, addr-fs.mbW-1)
	}
	out.TopLeftAvailable = tl
	if tl {
		out.TopLeft = pl.Pix(x-1, y-1)
	}
	if top {
		for i := 0; i < 8; i++ {
			out.Top[i] = pl.Pix(x+i, y-1)
		}
		for i := 8; i < 16; i++ {
			out.Top[i] = out.Top[7]
			if tr {
				out.Top[i] = pl.Pix(x+i, y-1)
			}
		}
	}
	if left {
		for i := 0; i < 8; i++ {
			out.Left[i] = pl.Pix(x-1, y+i)
		}
	}
	return out, top, left
}

func (fs *frameState) addLuma8Block(mbx, mby, block, qp int, levels *[64]int32, intra bool) error {
	var coeff, residual [64]int32
	if err := transform.Dequant8x8(&coeff, levels, qp, fs.matrix8(intra)); err != nil {
		return err
	}
	if err := transform.IDCT8x8(&residual, &coeff); err != nil {
		return err
	}
	x, y := mbx*16+(block%2)*8, mby*16+(block/2)*8
	pl := &fs.pic.Planes[0]
	for yy := 0; yy < 8; yy++ {
		for xx := 0; xx < 8; xx++ {
			pl.SetPix(x+xx, y+yy, clip8(int(pl.Pix(x+xx, y+yy))+int(residual[yy*8+xx])))
		}
	}
	return nil
}

func (fs *frameState) reconstructIntra8x8(mbx, mby, cbp, qp int, res *residual) error {
	pl := &fs.pic.Planes[0]
	info := &fs.info[mby*fs.mbW+mbx]
	for block := 0; block < 4; block++ {
		bx, by := block%2, block/2
		s, top, left := fs.intra8Samples(mbx, mby, bx, by)
		x, y := mbx*16+bx*8, mby*16+by*8
		mode := int(info.intraModes[by*8+bx*2])
		if err := prediction.Intra8x8(pl.Data[pl.Index(x, y):], pl.Stride, mode, s, top, left); err != nil {
			return err
		}
		if cbp&(1<<block) != 0 {
			if err := fs.addLuma8Block(mbx, mby, block, qp, &res.luma8[block], true); err != nil {
				return err
			}
		}
	}
	return nil
}
