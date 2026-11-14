package encoder

import (
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cavlc"
)

// Spec: table 9-4 的 Intra CBP 映射；值是 luma 位图 | chroma<<4。
var intraCBP = [48]int{47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14, 39, 43, 45, 46, 16, 3, 5, 10, 12, 19, 21, 26, 28, 35, 37, 42, 44, 1, 2, 4, 8, 17, 18, 20, 24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41}

// Spec: 表9-4，ChromaArrayType=0/3 的 CBP 只有四个亮度区域位。
var monochromeIntraCBP = [16]int{15, 0, 7, 11, 13, 14, 3, 5, 10, 12, 1, 2, 4, 8, 6, 9}
var monochromeInterCBP = [16]int{0, 1, 2, 4, 8, 3, 5, 10, 12, 15, 7, 11, 13, 14, 6, 9}

func (s *frameState) writeMB(w *bitstream.Writer, addr int, qp *int, res *residual) error {
	info := &s.info[addr]
	offset := 0
	if s.reference != nil {
		offset = 5
	}
	if info.is16 {
		typ := 1 + info.mode16 + (info.cbp>>4)*4
		if info.cbp&15 != 0 {
			typ += 12
		}
		w.WriteUE(uint32(typ + offset))
	} else {
		w.WriteUE(uint32(offset))
		if s.allow8 {
			w.WriteBit(uint8(flag(info.transform8)))
		}
		count := 16
		if info.transform8 {
			count = 4
		}
		for n := 0; n < count; n++ {
			index := n
			if info.transform8 {
				index = n * 4
			}
			r := raster(index)
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
	if s.chromaArrayType() == 1 || s.chromaArrayType() == 2 {
		w.WriteUE(uint32(info.chromaMode))
	}
	if !info.is16 {
		table := intraCBP[:]
		if s.chromaArrayType() == 0 || s.chromaArrayType() == 3 {
			table = monochromeIntraCBP[:]
		}
		for code, cbp := range table {
			if cbp == info.cbp {
				w.WriteUE(uint32(code))
				break
			}
		}
	}
	if info.is16 || info.cbp != 0 {
		delta := s.qpDelta(info.qp, *qp)
		w.WriteSE(int32(delta))
		*qp = info.qp
	} else {
		info.qp = *qp
	}
	return s.writeResidual(w, addr, res)
}

// writeResidual 是 I/P 共用的 CAVLC 残差写出，不根据 slice 类型另维护一份。
func (s *frameState) writeResidual(w *bitstream.Writer, addr int, res *residual) error {
	info := &s.info[addr]
	if info.is16 {
		if _, err := s.encodeResidualBlock(w, &res.lumaDC, cavlc.LumaDC, s.nc(addr, 0, 0)); err != nil {
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
		levels := &res.luma[r]
		var stream [16]int32
		if info.transform8 {
			// Spec: 8.5，CAVLC把一个8×8扫描交织为四个4×4子流。
			for k := 0; k < 16; k++ {
				stream[s.scan4()[k]] = res.luma8[i8][s.scan8()[4*k+n%4]]
			}
			levels = &stream
		}
		count, err := s.encodeResidualBlock(w, levels, typ, s.nc(addr, r, 0))
		if err != nil {
			return err
		}
		s.nz[addr*16+r] = uint8(count)
	}
	if s.chromaArrayType() == 0 {
		return nil
	}
	if s.chromaArrayType() == 3 {
		return s.writeChroma444(w, addr, res)
	}
	cbp := info.cbp >> 4
	if cbp != 0 {
		for c := 0; c < 2; c++ {
			var block [16]int32
			copy(block[:], res.chromaDC[c][:])
			typ := cavlc.ChromaDC
			if s.chromaArrayType() == 2 {
				typ = cavlc.ChromaDC422
			}
			if _, err := s.encodeResidualBlock(w, &block, typ, 0); err != nil {
				return err
			}
		}
	}
	if cbp == 2 {
		for c := 0; c < 2; c++ {
			width, height := s.planeMBSize(c + 1)
			for r := range width * height / 16 {
				count, err := s.encodeResidualBlock(w, &res.chroma[c][r], cavlc.LumaAC, s.nc(addr, r, c+1))
				if err != nil {
					return err
				}
				s.cnz[c][addr*16+r] = uint8(count)
			}
		}
	}
	return nil
}

func (s *frameState) writeChroma444(w *bitstream.Writer, addr int, res *residual) error {
	info := &s.info[addr]
	for c := range 2 {
		if info.is16 {
			if _, err := s.encodeResidualBlock(w, &res.chromaDC[c], cavlc.LumaDC, s.nc(addr, 0, c+1)); err != nil {
				return err
			}
		}
		for n := range 16 {
			r, block := raster(n), n/4
			if info.cbp&(1<<block) == 0 {
				continue
			}
			typ := cavlc.Luma4x4
			if info.is16 {
				typ = cavlc.LumaAC
			}
			levels := &res.chroma[c][r]
			var stream [16]int32
			if info.transform8 {
				for k := range 16 {
					stream[s.scan4()[k]] = res.chroma8[c][block][s.scan8()[4*k+n%4]]
				}
				levels = &stream
			}
			count, err := s.encodeResidualBlock(w, levels, typ, s.nc(addr, r, c+1))
			if err != nil {
				return err
			}
			s.cnz[c][addr*16+r] = uint8(count)
		}
	}
	return nil
}
