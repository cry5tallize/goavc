package encoder

import (
	"bytes"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/nal"
)

// cabacWriter 的上下文跨 PCM restart 保留，算术区间则重新初始化。
type cabacWriter struct {
	e             *cabac.Encoder
	ctx           [cabac.ContextCount]cabac.Context
	ctx444        *[cabac.ContextCount444]cabac.Context
	lastDelta     bool
	completedBins uint64
}

func newCABACWriter(e *cabac.Encoder, intra bool, qp int, chroma444 bool) (*cabacWriter, error) {
	c := &cabacWriter{e: e}
	if chroma444 {
		ctx, err := cabac.InitContexts444(intra, 0, min(51, max(0, qp)))
		if err != nil {
			return nil, err
		}
		c.ctx444 = &ctx
	} else {
		ctx, err := cabac.InitContexts(intra, 0, min(51, max(0, qp)))
		if err != nil {
			return nil, err
		}
		c.ctx = ctx
	}
	return c, nil
}

func (c *cabacWriter) bin(index, value int) {
	if c.ctx444 != nil {
		c.e.Bin(&c.ctx444[index], uint8(value))
	} else {
		c.e.Bin(&c.ctx[index], uint8(value))
	}
}

func flag(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (e *Encoder) encodeCABAC(src *picture.Picture, intra, idr bool) ([]byte, *frameState, error) {
	s, err := e.newFrameState(src)
	if err != nil {
		return nil, nil, err
	}
	if !idr {
		s.reference = e.reference
	}
	var out bytes.Buffer
	aw := nal.NewAnnexBWriter(&out)
	emit := func(typ nal.UnitType, rbsp []byte) error {
		return aw.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)})
	}
	if idr && (!e.separateColourPlane || e.colourPlaneID == 0) {
		sps, err := e.sps.MarshalRBSP()
		if err != nil {
			return nil, nil, err
		}
		pps, err := e.pps.MarshalRBSP(e.sps)
		if err != nil {
			return nil, nil, err
		}
		if err = emit(nal.TypeSPS, sps); err != nil {
			return nil, nil, err
		}
		if err = emit(nal.TypePPS, pps); err != nil {
			return nil, nil, err
		}
	}
	for slice := 0; slice < e.slices; slice++ {
		first, last := len(s.info)*slice/e.slices, len(s.info)*(slice+1)/e.slices
		s.mv.BeginSlice(first)
		w := bitstream.NewWriter()
		if intra {
			e.sliceHeader(w, first, e.pcm, idr)
		} else {
			e.pSliceHeader(w, first, s.weights)
		}
		for w.BitsWritten()%8 != 0 {
			w.WriteBit(1)
		}
		ce, err := cabac.NewEncoder(w)
		if err != nil {
			return nil, nil, err
		}
		c, err := newCABACWriter(ce, intra, e.frameQP, s.chromaArrayType() == 3)
		if err != nil {
			return nil, nil, err
		}
		qp := e.frameQP
		for addr := first; addr < last; addr++ {
			s.info[addr].start = first
			var res residual
			if e.pcm {
				s.info[addr].pcm = true
			} else if intra {
				res, err = s.analyzeIntraRD(addr, qp)
			} else {
				res, err = s.analyzeRD(addr, qp, e.searchRange, e.fullSearch)
			}
			if err != nil {
				return nil, nil, err
			}
			// RD 的 NZ 不是已经写出的语法；当前块统计必须重新按扫描序提交。
			clear(s.nz[addr*16 : addr*16+16])
			for comp := range s.cnz {
				clear(s.cnz[comp][addr*16 : addr*16+16])
			}
			if !intra {
				c.skip(s, addr)
			}
			if !s.info[addr].skip {
				c.mbType(s, addr, intra)
				if s.info[addr].pcm {
					if err = c.e.Err(); err != nil {
						return nil, nil, err
					}
					s.writePCMSamples(w, addr)
					c.completedBins += c.e.BinCount()
					c.e, err = cabac.NewEncoder(w)
					if err != nil {
						return nil, nil, err
					}
					c.lastDelta = false
				} else {
					c.macroblock(s, addr, &qp, &res)
				}
			} else {
				c.lastDelta = false
			}
			c.e.Terminate(uint8(flag(addr == last-1)))
			if err = c.e.Err(); err != nil {
				return nil, nil, err
			}
		}
		typ := nal.TypeSlice
		if idr {
			typ = nal.TypeSliceIDR
		}
		payload := cabacZeroWordPayloadFormat(w.Bytes(), c.completedBins+c.e.BinCount(), last-first, s.rawMBBits())
		if err = aw.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: payload}); err != nil {
			return nil, nil, err
		}
	}
	s.mv.BeginSlice(0)
	if !e.pcm {
		s.deblock()
	}
	s.src, s.reference = nil, nil
	s.searchReference = nil
	return out.Bytes(), s, nil
}

func (s *frameState) cabacNeighbors(addr int) [2]int {
	a, b := -1, -1
	if addr%s.pic.MBWidth != 0 && addr-1 >= s.info[addr].start {
		a = addr - 1
	}
	if addr-s.pic.MBWidth >= s.info[addr].start {
		b = addr - s.pic.MBWidth
	}
	return [2]int{a, b}
}

func (c *cabacWriter) skip(s *frameState, addr int) {
	inc := 0
	for _, n := range s.cabacNeighbors(addr) {
		if n >= 0 && !s.info[n].skip {
			inc++
		}
	}
	c.bin(11+inc, flag(s.info[addr].skip))
}

// Spec: 9.3.3，I/P 的 mb_type 二叉树；I16 的 CBP 隐含在类型中。
func (c *cabacWriter) mbType(s *frameState, addr int, intra bool) {
	i := &s.info[addr]
	if !intra {
		c.bin(14, flag(!i.inter))
		if i.inter {
			c.bin(15, flag(i.partition == 1 || i.partition == 2))
			if i.partition == 0 || i.partition == 3 {
				c.bin(16, flag(i.partition == 3))
			} else {
				c.bin(17, flag(i.partition == 1))
			}
			return
		}
	}
	base, inc := 17, 0
	if intra {
		base = 3
		for _, n := range s.cabacNeighbors(addr) {
			if n >= 0 && (s.info[n].is16 || s.info[n].pcm) {
				inc++
			}
		}
	}
	c.bin(base+inc, flag(i.is16 || i.pcm))
	if !i.is16 && !i.pcm {
		return
	}
	c.e.Terminate(uint8(flag(i.pcm)))
	if i.pcm {
		return
	}
	if intra {
		base += 2
	}
	c.bin(base+1, flag(i.cbp&15 != 0))
	// mb_type的I16二叉树不随ChromaArrayType缩短；即使没有
	// 独立色度CBP语法，仍须写出其值为0的类型分支bin。
	c.bin(base+2, flag(i.cbp>>4 != 0))
	if i.cbp>>4 != 0 {
		c.bin(base+2+flag(intra), flag(i.cbp>>4 == 2))
	}
	c.bin(base+3+flag(intra), i.mode16>>1)
	c.bin(base+3+2*flag(intra), i.mode16&1)
}

func (c *cabacWriter) macroblock(s *frameState, addr int, qp *int, res *residual) {
	i := &s.info[addr]
	if i.inter {
		c.motion(s, addr)
	} else {
		if !i.is16 {
			if s.allow8 {
				c.transformFlag(s, addr)
			}
			count := 16
			if i.transform8 {
				count = 4
			}
			for n := 0; n < count; n++ {
				index := n
				if i.transform8 {
					index = n * 4
				}
				r := raster(index)
				mode, mpm := int(i.modes[r]), s.probable(addr, r)
				c.bin(68, flag(mode == mpm))
				if mode != mpm {
					if mode > mpm {
						mode--
					}
					for b := 0; b < 3; b++ {
						c.bin(69, (mode>>b)&1)
					}
				}
			}
		}
		if chroma := s.chromaArrayType(); chroma == 1 || chroma == 2 {
			inc := 0
			for _, n := range s.cabacNeighbors(addr) {
				if n >= 0 && !s.info[n].inter && !s.info[n].pcm && s.info[n].chromaMode > 0 {
					inc++
				}
			}
			c.bin(64+inc, flag(i.chromaMode > 0))
			if i.chromaMode > 0 {
				for v := 1; v < 3; v++ {
					c.bin(67, flag(i.chromaMode > v))
					if i.chromaMode == v {
						break
					}
				}
			}
		}
	}
	c.residualMacroblock(s, addr, qp, res)
}

func (c *cabacWriter) residualMacroblock(s *frameState, addr int, qp *int, res *residual) {
	i := &s.info[addr]
	if !i.is16 {
		c.cbp(s, addr)
	}
	if s.allow8 && i.inter && i.cbp&15 != 0 {
		c.transformFlag(s, addr)
	}
	if i.is16 || i.cbp != 0 {
		delta := s.qpDelta(i.qp, *qp)
		v := 2*delta - 1
		if delta <= 0 {
			v = -2 * delta
		}
		c.bin(60+flag(c.lastDelta), flag(v != 0))
		if v != 0 {
			for k := 1; k <= v; k++ {
				index := 63
				if k == 1 {
					index = 62
				}
				c.bin(index, flag(k < v))
			}
		}
		c.lastDelta = delta != 0
		*qp = i.qp
	} else {
		i.qp = *qp
		c.lastDelta = false
	}
	c.residual(s, addr, res)
}

func (c *cabacWriter) cbp(s *frameState, addr int) {
	a, b := 15, 15
	n := s.cabacNeighbors(addr)
	if n[0] >= 0 {
		a = s.info[n[0]].cbp
	}
	if n[1] >= 0 {
		b = s.info[n[1]].cbp
	}
	v := s.info[addr].cbp
	c.bin(73+flag(a&2 == 0)+2*flag(b&4 == 0), v&1)
	c.bin(73+flag(v&1 == 0)+2*flag(b&8 == 0), (v>>1)&1)
	c.bin(73+flag(a&8 == 0)+2*flag(v&1 == 0), (v>>2)&1)
	c.bin(73+flag(v&4 == 0)+2*flag(v&2 == 0), (v>>3)&1)
	if chroma := s.chromaArrayType(); chroma != 1 && chroma != 2 {
		return
	}
	ac, bc := a>>4, b>>4
	c.bin(77+flag(ac > 0)+2*flag(bc > 0), flag(v>>4 > 0))
	if v>>4 > 0 {
		c.bin(81+flag(ac == 2)+2*flag(bc == 2), flag(v>>4 == 2))
	}
}
