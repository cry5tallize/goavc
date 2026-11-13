package encoder

import (
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cavlc"
)

// Spec: table 9-4 的 Intra CBP 映射；值是 luma 位图 | chroma<<4。
var intraCBP = [48]int{47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14, 39, 43, 45, 46, 16, 3, 5, 10, 12, 19, 21, 26, 28, 35, 37, 42, 44, 1, 2, 4, 8, 17, 18, 20, 24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41}

func (s *intraState) writeMB(w *bitstream.Writer, addr int, qp *int, res *intraResidual) error {
	info := &s.info[addr]
	if info.is16 {
		typ := 1 + info.mode16 + (info.cbp>>4)*4
		if info.cbp&15 != 0 {
			typ += 12
		}
		w.WriteUE(uint32(typ))
	} else {
		w.WriteUE(0)
		for n := 0; n < 16; n++ {
			r := raster(n)
			mode := int(info.modes[r])
			mpm := s.probable(addr, r)
			if mode == mpm {
				w.WriteBit(1)
			} else {
				w.WriteBit(0)
				rem := mode
				if mode > mpm {
					rem--
				}
				w.WriteBits(3, uint64(rem))
			}
		}
	}
	w.WriteUE(uint32(info.chromaMode))
	if !info.is16 {
		for code, cbp := range intraCBP {
			if cbp == info.cbp {
				w.WriteUE(uint32(code))
				break
			}
		}
	}
	if info.is16 || info.cbp != 0 {
		delta := info.qp - *qp
		if delta > 25 {
			delta -= 52
		}
		if delta < -26 {
			delta += 52
		}
		w.WriteSE(int32(delta))
		*qp = info.qp
	} else {
		info.qp = *qp
	}
	if info.is16 {
		if _, err := cavlc.EncodeBlock(w, &res.lumaDC, cavlc.LumaDC, s.nc(addr, 0, 0)); err != nil {
			return err
		}
	}
	for n := 0; n < 16; n++ {
		r := raster(n)
		i8 := n / 4
		if info.cbp&(1<<i8) == 0 {
			continue
		}
		typ := cavlc.Luma4x4
		if info.is16 {
			typ = cavlc.LumaAC
		}
		count, err := cavlc.EncodeBlock(w, &res.luma[r], typ, s.nc(addr, r, 0))
		if err != nil {
			return err
		}
		s.nz[addr*16+r] = uint8(count)
	}
	cbp := info.cbp >> 4
	if cbp != 0 {
		for c := 0; c < 2; c++ {
			var block [16]int32
			copy(block[:4], res.chromaDC[c][:])
			if _, err := cavlc.EncodeBlock(w, &block, cavlc.ChromaDC, 0); err != nil {
				return err
			}
		}
	}
	if cbp == 2 {
		for c := 0; c < 2; c++ {
			for r := 0; r < 4; r++ {
				count, err := cavlc.EncodeBlock(w, &res.chroma[c][r], cavlc.LumaAC, s.nc(addr, r, c+1))
				if err != nil {
					return err
				}
				s.cnz[c][addr*4+r] = uint8(count)
			}
		}
	}
	return nil
}
