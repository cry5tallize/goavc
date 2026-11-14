package encoder

import (
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/transform"
)

// cbf 按 Spec: 9.3.3.1.1.9 推导已编码邻块；不可用时帧内取 1，帧间取 0。
func (s *frameState) cbf(addr, bx, by, comp int, dc bool) (bool, bool) {
	var values [2]bool
	for side := 0; side < 2; side++ {
		x, y, other := bx, by, addr
		if dc {
			other = s.cabacNeighbors(addr)[side]
		} else {
			n := 4
			if comp >= 0 {
				n = 2
			}
			if side == 0 {
				x--
				if x < 0 {
					x = n - 1
					other = s.cabacNeighbors(addr)[0]
				}
			} else {
				y--
				if y < 0 {
					y = n - 1
					other = s.cabacNeighbors(addr)[1]
				}
			}
		}
		if other < 0 {
			values[side] = !s.info[addr].inter
			continue
		}
		if s.info[other].pcm {
			values[side] = true
			continue
		}
		if dc {
			index := 0
			if comp >= 0 {
				index = comp + 1
			}
			values[side] = s.dcCoded[index][other]
		} else if comp < 0 {
			values[side] = s.nz[other*16+y*4+x] != 0
		} else {
			values[side] = s.cnz[comp][other*4+y*2+x] != 0
		}
	}
	return values[0], values[1]
}

func (c *cabacWriter) residual(s *frameState, addr int, res *residual) {
	block := func(cat, bx, by, comp int, dc bool, coeff []int32, scan []int) uint8 {
		a, b := s.cbf(addr, bx, by, comp, dc)
		count := 0
		for _, index := range scan {
			if coeff[index] != 0 {
				count++
			}
		}
		c.bin(85+4*cat+flag(a)+2*flag(b), flag(count != 0))
		if count != 0 {
			c.e.Residual(&c.ctx, cat, coeff, scan)
		}
		return uint8(count)
	}
	i := &s.info[addr]
	if i.is16 {
		s.dcCoded[0][addr] = block(cabac.LumaDC, 0, 0, -1, true, res.lumaDC[:], transform.LumaDCScan[:]) != 0
	}
	for n := 0; n < 16; n++ {
		r := raster(n)
		if i.cbp&(1<<(n/4)) == 0 {
			continue
		}
		cat, scan := cabac.Luma4x4, transform.Zigzag4x4[:]
		if i.is16 {
			cat, scan = cabac.LumaAC, scan[1:]
		}
		s.nz[addr*16+r] = block(cat, r%4, r/4, -1, false, res.luma[r][:], scan)
	}
	if i.cbp>>4 != 0 {
		for comp := 0; comp < 2; comp++ {
			s.dcCoded[comp+1][addr] = block(cabac.ChromaDC, 0, 0, comp, true, res.chromaDC[comp][:], transform.DC2x2Scan[:]) != 0
		}
	}
	if i.cbp>>4 == 2 {
		for comp := 0; comp < 2; comp++ {
			for r := 0; r < 4; r++ {
				s.cnz[comp][addr*4+r] = block(cabac.ChromaAC, r%2, r/2, comp, false, res.chroma[comp][r][:], transform.Zigzag4x4[1:])
			}
		}
	}
}
