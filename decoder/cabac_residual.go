package decoder

import (
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/transform"
)

// cbfNeighbor 推导 CABAC coded_block_flag 的邻域条件。帧内当前宏块
// 的不可用邻块推导为 1；帧间推导为 0（Spec: 9.3.3.1.1.9）。
func (fs *frameState) cbfNeighbor(addr, mbx, mby, bx, by, comp int, dc bool) (bool, bool) {
	values := [2]bool{}
	for side := 0; side < 2; side++ {
		x, y := bx, by
		other := addr
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
			n := 4
			if comp >= 0 {
				n = 2
			}
			if side == 0 {
				x--
				if x < 0 {
					x = n - 1
					other--
					if mbx == 0 {
						other = -1
					}
				}
			} else {
				y--
				if y < 0 {
					y = n - 1
					other -= fs.mbW
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
		if dc {
			index := 0
			if comp >= 0 {
				index = comp + 1
			}
			values[side] = fs.dcCoded[index][other]
		} else if comp < 0 {
			values[side] = fs.lumaNZ[other*16+y*4+x] != 0
		} else {
			values[side] = fs.chromaNZ[comp][other*4+y*2+x] != 0
		}
	}
	return values[0], values[1]
}

func (fs *frameState) decodeCABACResidual(s *sliceEntry, addr, mbx, mby int, i16, lumaAC bool, cbp int, res *residual) error {
	c := s.cabac
	block := func(cat, bx, by, comp int, dc bool, dst []int32, scan []int) (int, error) {
		a, b := fs.cbfNeighbor(addr, mbx, mby, bx, by, comp, dc)
		if c.bin(85+4*cat+boolToInt(a)+2*boolToInt(b)) == 0 {
			return 0, c.d.Err()
		}
		return c.d.Residual(&c.ctx, cat, dst, scan)
	}
	if i16 {
		n, err := block(cabac.LumaDC, 0, 0, -1, true, res.lumaDC[:], transform.LumaDCScan[:])
		if err != nil {
			return err
		}
		fs.dcCoded[0][addr] = n != 0
	}
	for n := 0; n < 16; n++ {
		r := blockRaster(n)
		if i16 && !lumaAC || !i16 && cbp&(1<<(n/4)) == 0 {
			continue
		}
		cat, scan := cabac.Luma4x4, transform.Zigzag4x4[:]
		if i16 {
			cat = cabac.LumaAC
			scan = scan[1:]
		}
		count, err := block(cat, r%4, r/4, -1, false, res.luma[r][:], scan)
		if err != nil {
			return err
		}
		fs.lumaNZ[addr*16+r] = uint8(count)
	}
	chroma := (cbp >> 4) & 3
	if chroma > 0 {
		for comp := 0; comp < 2; comp++ {
			count, err := block(cabac.ChromaDC, 0, 0, comp, true, res.chromaDC[comp][:], transform.DC2x2Scan[:])
			if err != nil {
				return err
			}
			fs.dcCoded[comp+1][addr] = count != 0
		}
	}
	if chroma == 2 {
		for comp := 0; comp < 2; comp++ {
			for b := 0; b < 4; b++ {
				count, err := block(cabac.ChromaAC, b%2, b/2, comp, false, res.chroma[comp][b][:], transform.Zigzag4x4[1:])
				if err != nil {
					return err
				}
				fs.chromaNZ[comp][addr*4+b] = uint8(count)
			}
		}
	}
	return c.d.Err()
}
