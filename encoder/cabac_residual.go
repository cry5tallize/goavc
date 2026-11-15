package encoder

import (
	"github.com/cry5tallize/goavc/internal/entropy/cabac"
	"github.com/cry5tallize/goavc/internal/transform"
)

// cbf 按 Spec: 9.3.3.1.1.9 推导已编码邻块；不可用时帧内取 1，帧间取 0。
func (s *frameState) cbf(addr, bx, by, comp int, dc, eight bool) (bool, bool) {
	var values [2]bool
	for side := 0; side < 2; side++ {
		x, y, other := bx, by, addr
		if dc {
			other = s.cabacNeighbors(addr)[side]
		} else {
			width, height := 4, 4
			if comp >= 0 && s.chromaArrayType() != 3 {
				width = 2
				if s.chromaArrayType() == 1 {
					height = 2
				}
			}
			if side == 0 {
				x--
				if x < 0 {
					x = width - 1
					other = s.cabacNeighbors(addr)[0]
				}
			} else {
				y--
				if y < 0 {
					y = height - 1
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
		// 8x8的transBlock不能由邻宏块的4x4非零统计代替。
		// 邻宏块本身可用但不采用8x8时，condTermFlag为0；
		// 只有宏块不可用才应用上方的帧内默认值。
		if eight && !s.info[other].transform8 {
			continue
		}
		// Luma/Cb/Cr的I16 DC只在邻宏块采用I16时有transBlock；
		// 420/422的ChromaDC则不受帧内类型限制。
		if dc && (comp < 0 || s.chromaArrayType() == 3) && !s.info[other].is16 {
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
			width := 4
			if s.chromaArrayType() != 3 {
				width = 2
			}
			values[side] = s.cnz[comp][other*16+y*width+x] != 0
		}
	}
	return values[0], values[1]
}

// writeResidual 保留 420/422 的 460 模型域，非独立 444 才访问扩展上下文。
func (c *cabacWriter) writeResidual(cat int, coeff []int32, scan []int) int {
	if c.ctx444 != nil {
		count, _ := c.e.Residual444(c.ctx444, cat, coeff, scan, c.field)
		return count
	}
	if c.field {
		count, _ := c.e.ResidualField(&c.ctx, cat, coeff, scan)
		return count
	}
	count, _ := c.e.Residual(&c.ctx, cat, coeff, scan)
	return count
}

func (c *cabacWriter) residualBlock(s *frameState, addr, cat, bx, by, comp int, dc bool, coeff []int32, scan []int) uint8 {
	a, b := s.cbf(addr, bx, by, comp, dc, cat == cabac.Luma8x8 || cat == cabac.Cb8x8 || cat == cabac.Cr8x8)
	count := uint8(0)
	for _, index := range scan {
		if coeff[index] != 0 {
			count++
		}
	}
	// 444 的 DC/AC/8x8 基址并不连续，统一由熵内核的规范表选择。
	base, _ := cabac.CodedBlockFlagBase(cat)
	c.bin(base+flag(a)+2*flag(b), flag(count != 0))
	if count != 0 {
		c.writeResidual(cat, coeff, scan)
	}
	return count
}

var cabacPlaneCategories = [3][4]int{
	{cabac.LumaDC, cabac.LumaAC, cabac.Luma4x4, cabac.Luma8x8},
	{cabac.CbDC, cabac.CbAC, cabac.Cb4x4, cabac.Cb8x8},
	{cabac.CrDC, cabac.CrAC, cabac.Cr4x4, cabac.Cr8x8},
}

// residualPlane 对 444 三平面使用相同的亮度语法，CBP 与预测类型由宏块共享。
func (c *cabacWriter) residualPlane(s *frameState, addr int, res *residual, comp int) {
	cat := &cabacPlaneCategories[comp+1]
	dc, coeff, coeff8, nz := res.lumaDC[:], &res.luma, &res.luma8, s.nz
	if comp >= 0 {
		dc, coeff, coeff8, nz = res.chromaDC[comp][:], &res.chroma[comp], &res.chroma8[comp], s.cnz[comp]
	}
	i := &s.info[addr]
	if i.is16 {
		s.dcCoded[comp+1][addr] = c.residualBlock(s, addr, cat[0], 0, 0, comp, true, dc, s.scanDC()) != 0
	}
	if i.transform8 {
		for b := range 4 {
			if i.cbp&(1<<b) == 0 {
				continue
			}
			count := uint8(0)
			if s.chromaArrayType() == 3 {
				count = c.residualBlock(s, addr, cat[3], (b%2)*2, (b/2)*2, comp, false, coeff8[b][:], s.scan8())
			} else {
				// 420/422 及独立色平面的亮度 8x8 不发送 coded_block_flag。
				count = uint8(c.writeResidual(cat[3], coeff8[b][:], s.scan8()))
			}
			for k := range 4 {
				nz[addr*16+raster(b*4+k)] = count
			}
		}
		return
	}
	for n := range 16 {
		if i.cbp&(1<<(n/4)) == 0 {
			continue
		}
		r := raster(n)
		category, scan := cat[2], s.scan4()
		if i.is16 {
			category, scan = cat[1], scan[1:]
		}
		nz[addr*16+r] = c.residualBlock(s, addr, category, r%4, r/4, comp, false, coeff[r][:], scan)
	}
}

func (c *cabacWriter) residual(s *frameState, addr int, res *residual) {
	c.residualPlane(s, addr, res, -1)
	chroma := s.chromaArrayType()
	if chroma == 0 {
		return
	}
	if chroma == 3 {
		for comp := range 2 {
			c.residualPlane(s, addr, res, comp)
		}
		return
	}
	i := &s.info[addr]
	scan, blocks := transform.DC2x2Scan[:], 4
	if chroma == 2 {
		scan, blocks = transform.DC2x4Scan[:], 8
	}
	if i.cbp>>4 != 0 {
		for comp := range 2 {
			s.dcCoded[comp+1][addr] = c.residualBlock(s, addr, cabac.ChromaDC, 0, 0, comp, true, res.chromaDC[comp][:], scan) != 0
		}
	}
	if i.cbp>>4 == 2 {
		for comp := range 2 {
			for r := range blocks {
				s.cnz[comp][addr*16+r] = c.residualBlock(s, addr, cabac.ChromaAC, r%2, r/2, comp, false, res.chroma[comp][r][:], s.scan4()[1:])
			}
		}
	}
}
