package decoder

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/entropy/cabac"
	"github.com/cry5tallize/goavc/syntax"
)

func decodeCABACSlice(fs *frameState, s *sliceEntry, sliceID, first, end int) error {
	for !s.data.ByteAligned() {
		v, err := s.data.ReadBits(1)
		if err != nil {
			return err
		}
		if v != 1 {
			return fmt.Errorf("decoder: CABAC alignment bit is not one: %w", h264.ErrInvalidData)
		}
	}
	engine, err := cabac.New(s.data)
	if err != nil {
		return err
	}
	// CABAC上下文初始化使用语法QPY裁剪域，不使用变换的QP'。
	s.cabac = &cabacSyntax{d: engine}
	if fs.sps.ChromaFormatIDC == 3 && !fs.sps.SeparateColourPlaneFlag {
		ctx, err := cabac.InitContexts444(s.hdr.Type == syntax.SliceTypeI, s.hdr.CabacInitIDC, max(0, min(51, s.hdr.SliceQP)))
		if err != nil {
			return err
		}
		s.cabac.ctx = ctx
	} else {
		ctx, err := cabac.InitContexts(s.hdr.Type == syntax.SliceTypeI, s.hdr.CabacInitIDC, max(0, min(51, s.hdr.SliceQP)))
		if err != nil {
			return err
		}
		copy(s.cabac.ctx[:], ctx[:])
	}
	if fs.mbaff {
		return decodeMBAFFCABAC(fs, s, sliceID, first, end)
	}
	fs.mv.BeginSlice(first)
	if fs.mv1 != nil {
		fs.mv1.BeginSlice(first)
	}
	qp := s.hdr.SliceQP
	for addr := first; addr < end; addr++ {
		fs.info[addr].sliceID = sliceID
		skip := false
		if s.hdr.Type == syntax.SliceTypeP || s.hdr.Type == syntax.SliceTypeB {
			inc := 0
			for side, other := range []int{addr - 1, addr - fs.mbW} {
				if side == 0 && addr%fs.mbW == 0 {
					continue
				}
				if fs.neighborAvailable(addr, other) && !fs.info[other].skip {
					inc++
				}
			}
			base := 11
			if s.hdr.Type == syntax.SliceTypeB {
				base = 24
			}
			skip = s.cabac.bin(base+inc) != 0
		}
		if engine.Err() != nil {
			return engine.Err()
		}
		if skip {
			s.cabac.lastDelta = false
			err = decodeSkipMacroblock(fs, addr, s, qp, sliceID)
		} else {
			err = decodeMacroblock(fs, addr, s, sliceID, &qp)
			if fs.info[addr].cbp == 0 && !fs.info[addr].intra16 {
				s.cabac.lastDelta = false
			}
		}
		if err != nil {
			return fmt.Errorf("decoder: CABAC mb %d: %w", addr, err)
		}
		fs.decoded[addr] = true
		engine = s.cabac.d
		last := engine.Terminate() != 0
		if engine.Err() != nil {
			return engine.Err()
		}
		if last != (addr == end-1) {
			return fmt.Errorf("decoder: CABAC slice termination at mb %d: %w", addr, h264.ErrInvalidData)
		}
	}
	return nil
}
