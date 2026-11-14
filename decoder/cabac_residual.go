package decoder

import (
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/transform"
)

// cbfNeighbor 推导 CABAC coded_block_flag 的邻域条件。帧内当前宏块
// 的不可用邻块推导为 1；帧间推导为 0（Spec: 9.3.3.1.1.9）。
func (fs *frameState) cbfNeighbor(addr, mbx, mby, bx, by, comp int, dc, eight bool) (bool, bool) {
	values := [2]bool{}
	for side := 0; side < 2; side++ {
		x, y := bx, by
		other := addr
		if fs.mbaff {
			if dc {
				other = fs.neighborMB(addr, side == 0)
			} else {
				plane := comp + 1
				if comp < 0 {
					plane = 0
				}
				other, x, y = fs.blockNeighbor(addr, plane, bx, by, side == 0)
			}
		} else {
			if dc {
				if side == 0 {
					other--
					if mbx == 0 {
						other = -1
					}
				} else {
					other -= fs.mbW
				}
			} else {
				w, h := 4, 4
				if comp >= 0 {
					w, h = fs.chromaW/4, fs.chromaH/4
				}
				if side == 0 {
					x--
					if x < 0 {
						x = w - 1
						other--
						if mbx == 0 {
							other = -1
						}
					}
				} else {
					y--
					if y < 0 {
						y = h - 1
						other -= fs.mbW
					}
				}
			}
		}
		if other != addr && !fs.neighborAvailable(addr, other) {
			values[side] = fs.info[addr].intra
			continue
		}
		if fs.info[other].pcm {
			values[side] = true
			continue
		}
		// Spec: 9.3.3.1.1.9：邻MB可用不代表所需变换块可用。
		// 8×8 CBF不能由邻MB的4×4系数推导；PCM已在上面单独处理。
		if eight && !fs.info[other].transform8 {
			continue
		}
		if dc && comp < 0 && !fs.info[other].intra16 {
			continue
		}
		if dc {
			index := 0
			if comp >= 0 {
				index = comp + 1
			}
			if fs.sps.ChromaFormatIDC == 3 && !fs.sps.SeparateColourPlaneFlag {
				index = fs.component
			}
			values[side] = fs.dcCoded[index][other]
		} else if comp < 0 {
			values[side] = fs.planeNZ()[other*16+y*4+x] != 0
		} else {
			values[side] = fs.chromaNZ[comp][other*fs.chromaBlocks+y*2+x] != 0
		}
	}
	return values[0], values[1]
}

// cabacBlock 共享CBF邻域推导，444使用扩展上下文且按当前分量选择类别。
func (fs *frameState) cabacBlock(s *sliceEntry, addr, mbx, mby, cat, bx, by, comp int, dc bool, dst []int32, scan []int) (int, error) {
	c := s.cabac
	a, b := fs.cbfNeighbor(addr, mbx, mby, bx, by, comp, dc, len(scan) == 64)
	base, err := cabac.CodedBlockFlagBase(cat)
	if err != nil {
		return 0, err
	}
	if c.bin(base+boolToInt(a)+2*boolToInt(b)) == 0 {
		return 0, c.d.Err()
	}
	return fs.cabacResidual(c, cat, dst, scan)
}

func (fs *frameState) decodeCABACPlane(s *sliceEntry, addr, mbx, mby int, i16, lumaAC bool, cbp int, res *planeResidual) error {
	c := s.cabac
	catDC, catAC, cat4, cat8 := cabac.LumaDC, cabac.LumaAC, cabac.Luma4x4, cabac.Luma8x8
	full := fs.sps.ChromaFormatIDC == 3 && !fs.sps.SeparateColourPlaneFlag
	if full && fs.component == 1 {
		catDC, catAC, cat4, cat8 = cabac.CbDC, cabac.CbAC, cabac.Cb4x4, cabac.Cb8x8
	}
	if full && fs.component == 2 {
		catDC, catAC, cat4, cat8 = cabac.CrDC, cabac.CrAC, cabac.Cr4x4, cabac.Cr8x8
	}
	grid := fs.planeNZ()
	if i16 {
		n, err := fs.cabacBlock(s, addr, mbx, mby, catDC, 0, 0, -1, true, res.lumaDC[:], fs.scanDC())
		if err != nil {
			return err
		}
		index := 0
		if full {
			index = fs.component
		}
		fs.dcCoded[index][addr] = n != 0
	}
	if fs.info[addr].transform8 {
		for block := 0; block < 4; block++ {
			if cbp&(1<<block) == 0 {
				continue
			}
			var count int
			var err error
			if full {
				count, err = fs.cabacBlock(s, addr, mbx, mby, cat8, block%2*2, block/2*2, -1, false, res.luma8[block][:], fs.scan8())
			} else {
				count, err = fs.cabacResidual(c, cat8, res.luma8[block][:], fs.scan8())
			}
			if err != nil {
				return err
			}
			for i := 0; i < 4; i++ {
				grid[addr*16+blockRaster(block*4+i)] = uint8(count)
			}
		}
	} else {
		for n := 0; n < 16; n++ {
			r := blockRaster(n)
			if i16 && !lumaAC || !i16 && cbp&(1<<(n/4)) == 0 {
				continue
			}
			cat, scan := cat4, fs.scan4()
			if i16 {
				cat, scan = catAC, scan[1:]
			}
			count, err := fs.cabacBlock(s, addr, mbx, mby, cat, r%4, r/4, -1, false, res.luma[r][:], scan)
			if err != nil {
				return err
			}
			grid[addr*16+r] = uint8(count)
		}
	}
	return c.d.Err()
}

func (fs *frameState) decodeCABACChroma(s *sliceEntry, addr, mbx, mby, cbp int, res *residual) error {
	chroma := (cbp >> 4) & 3
	if chroma > 0 {
		scan := transform.DC2x2Scan[:]
		if fs.chromaBlocks == 8 {
			scan = transform.DC2x4Scan[:]
		}
		for comp := 0; comp < 2; comp++ {
			count, err := fs.cabacBlock(s, addr, mbx, mby, cabac.ChromaDC, 0, 0, comp, true, res.chromaDC[comp][:], scan)
			if err != nil {
				return err
			}
			fs.dcCoded[comp+1][addr] = count != 0
		}
	}
	if chroma == 2 {
		for comp := 0; comp < 2; comp++ {
			for b := 0; b < fs.chromaBlocks; b++ {
				count, err := fs.cabacBlock(s, addr, mbx, mby, cabac.ChromaAC, b%2, b/2, comp, false, res.chroma[comp][b][:], fs.scan4()[1:])
				if err != nil {
					return err
				}
				fs.chromaNZ[comp][addr*fs.chromaBlocks+b] = uint8(count)
			}
		}
	}
	return s.cabac.d.Err()
}
