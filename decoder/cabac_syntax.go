package decoder

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/entropy/cabac"
	"github.com/cry5tallize/goavc/syntax"
)

// cabacSyntax 把算术 bin 映射到宏块语法值，不实现像素预测与重建。
// 编解码共享的数值内核仍由 residual.go 调用。
type cabacSyntax struct {
	d         *cabac.Decoder
	ctx       [cabac.ContextCount444]cabac.Context
	lastDelta bool
}

func (c *cabacSyntax) bin(i int) int { return int(c.d.Bin(&c.ctx[i])) }

func (c *cabacSyntax) intraType(fs *frameState, addr, base int, intra bool) int {
	inc := 0
	if intra {
		for side, other := range []int{addr - 1, addr - fs.mbW} {
			if fs.mbaff {
				other = fs.neighborMB(addr, side == 0)
			}
			if side == 0 && addr%fs.mbW == 0 {
				continue
			}
			if fs.neighborAvailable(addr, other) && (fs.info[other].intra16 || fs.info[other].pcm) {
				inc++
			}
		}
	}
	if c.bin(base+inc) == 0 {
		return 0
	}
	if c.d.Terminate() != 0 {
		return 25
	}
	if intra {
		base += 2
	}
	typ := 1 + 12*c.bin(base+1)
	if c.bin(base+2) != 0 {
		typ += 4 + 4*c.bin(base+2+boolToInt(intra))
	}
	typ += 2*c.bin(base+3+boolToInt(intra)) + c.bin(base+3+2*boolToInt(intra))
	return typ
}

func (s *sliceEntry) readMBType(fs *frameState, addr int) (uint32, error) {
	if s.cabac == nil {
		return s.data.ReadUE()
	}
	c := s.cabac
	if s.hdr.Type == syntax.SliceTypeI {
		return uint32(c.intraType(fs, addr, 3, true)), c.d.Err()
	}
	if s.hdr.Type == syntax.SliceTypeB {
		return uint32(c.bType(fs, addr)), c.d.Err()
	}
	if c.bin(14) != 0 {
		return uint32(5 + c.intraType(fs, addr, 17, false)), c.d.Err()
	}
	var typ int
	if c.bin(15) == 0 {
		typ = 3 * c.bin(16)
	} else {
		typ = 2 - c.bin(17)
	}
	return uint32(typ), c.d.Err()
}

func (s *sliceEntry) readIntraMode(pred int) (int, error) {
	if s.cabac == nil {
		flag, err := s.data.ReadBits(1)
		if err != nil {
			return 0, err
		}
		if flag != 0 {
			return pred, nil
		}
		v, err := s.data.ReadBits(3)
		return int(v) + boolToInt(int(v) >= pred), err
	}
	c := s.cabac
	if c.bin(68) != 0 {
		return pred, c.d.Err()
	}
	v := c.bin(69) + 2*c.bin(69) + 4*c.bin(69)
	return v + boolToInt(v >= pred), c.d.Err()
}

func (s *sliceEntry) readChromaMode(fs *frameState, addr int) (uint32, error) {
	if fs.sps.ChromaFormatIDC == 3 {
		return 0, nil
	}
	if s.cabac == nil {
		return s.data.ReadUE()
	}
	c := s.cabac
	inc := 0
	for i, other := range []int{addr - 1, addr - fs.mbW} {
		if fs.mbaff {
			other = fs.neighborMB(addr, i == 0)
		}
		if i == 0 && addr%fs.mbW == 0 {
			continue
		}
		if fs.neighborAvailable(addr, other) && fs.info[other].intra && !fs.info[other].pcm && fs.info[other].chromaMode > 0 {
			inc++
		}
	}
	mode := uint32(0)
	if c.bin(64+inc) != 0 {
		mode = 1
		for mode < 3 && c.d.Err() == nil && c.bin(67) != 0 {
			mode++
		}
	}
	return mode, c.d.Err()
}

func (s *sliceEntry) readCBP(fs *frameState, addr int, intra bool) (int, error) {
	if s.cabac == nil {
		if fs.sps.ChromaFormatIDC == 3 {
			v, err := s.data.ReadUE()
			if err != nil {
				return 0, err
			}
			if v > 15 {
				return 0, fmt.Errorf("decoder: full-plane coded_block_pattern %d: %w", v, h264.ErrInvalidData)
			}
			if intra {
				return int([16]uint8{15, 0, 7, 11, 13, 14, 3, 5, 10, 12, 1, 2, 4, 8, 6, 9}[v]), nil
			}
			return int([16]uint8{0, 1, 2, 4, 8, 3, 5, 10, 12, 15, 7, 11, 13, 14, 6, 9}[v]), nil
		}
		v, err := s.data.ReadUE()
		if err != nil {
			return 0, err
		}
		if v > 47 {
			return 0, fmt.Errorf("decoder: coded_block_pattern %d: %w", v, h264.ErrInvalidData)
		}
		if intra {
			return int(intraCBPMap[v]), nil
		}
		return int(interCBPMap[v]), nil
	}
	if fs.mbaff {
		return s.readMBAFFCBP(fs, addr)
	}
	c := s.cabac
	// 不可用邻块的 luma CBP 条件取 1，chroma CBP 条件取 0；
	// coded_block_flag 的不可用推导另由当前宏块是否帧内决定。
	a, b := 15, 15
	if addr%fs.mbW > 0 && fs.neighborAvailable(addr, addr-1) {
		a = fs.info[addr-1].cbp
	}
	if fs.neighborAvailable(addr, addr-fs.mbW) {
		b = fs.info[addr-fs.mbW].cbp
	}
	not := func(v int) int { return boolToInt(v == 0) }
	cbp := c.bin(73 + not(a&2) + 2*not(b&4))
	cbp |= c.bin(73+not(cbp&1)+2*not(b&8)) << 1
	cbp |= c.bin(73+not(a&8)+2*not(cbp&1)) << 2
	cbp |= c.bin(73+not(cbp&4)+2*not(cbp&2)) << 3
	if fs.sps.ChromaFormatIDC == 3 {
		return cbp, c.d.Err()
	}
	ac, bc := (a>>4)&3, (b>>4)&3
	if c.bin(77+boolToInt(ac > 0)+2*boolToInt(bc > 0)) != 0 {
		cbp |= (1 + c.bin(81+boolToInt(ac == 2)+2*boolToInt(bc == 2))) << 4
	}
	return cbp, c.d.Err()
}

func (s *sliceEntry) updateQP(qp *int) error {
	depthOffset := 6 * int(s.sps.BitDepthLumaMinus8)
	if s.cabac == nil {
		return updateQScale(s.data, qp, depthOffset)
	}
	c := s.cabac
	v := 0
	if c.bin(60+boolToInt(c.lastDelta)) != 0 {
		v = 1
		inc := 62
		for c.d.Err() == nil && c.bin(inc) != 0 {
			v++
			inc = 63
			if v > 2*(26+depthOffset/2) {
				return fmt.Errorf("decoder: CABAC qp delta overflow: %w", h264.ErrInvalidData)
			}
		}
	}
	delta := (v + 1) / 2
	if v%2 == 0 {
		delta = -delta
	}
	if err := applyQPDelta(qp, delta, depthOffset); err != nil {
		return err
	}
	c.lastDelta = delta != 0
	return c.d.Err()
}

func (s *sliceEntry) readPSubType() (uint32, error) {
	if s.cabac == nil {
		return s.data.ReadUE()
	}
	c := s.cabac
	if c.bin(21) != 0 {
		return 0, c.d.Err()
	}
	if c.bin(22) == 0 {
		return 1, c.d.Err()
	}
	if c.bin(23) != 0 {
		return 2, c.d.Err()
	}
	return 3, c.d.Err()
}
