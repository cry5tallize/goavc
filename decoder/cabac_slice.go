package decoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/syntax"
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
	ctx, err := cabac.InitContexts(s.hdr.Type == syntax.SliceTypeI, s.hdr.CabacInitIDC, s.hdr.SliceQP)
	if err != nil {
		return err
	}
	s.cabac = &cabacSyntax{d: engine, ctx: ctx}
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
