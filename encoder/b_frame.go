package encoder

import (
	"bytes"

	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
	"github.com/goavc/h264/nal"
)

// encodeB 使用可用的 spatial-direct 策略：本slice的全部宏块双列表ref_idx=0、
// MV=0，邻块预测仍为0，colocated-zero只会保持该结果。因此无需复制解码器
// 的direct推导算法；空间预测严格得出同一矢量。残差非零时写B_Direct，
// 量化残差全零时写B_Skip。B图片不进入参考列表。
func (e *Encoder) encodeB(src *picture.Picture) ([]byte, *frameState, error) {
	s, err := e.newFrameState(src)
	if err != nil {
		return nil, nil, err
	}
	s.reference = e.bPast
	s.weights = nil
	s.searchReference = nil
	var out bytes.Buffer
	aw := nal.NewAnnexBWriter(&out)
	for slice := 0; slice < e.slices; slice++ {
		first, last := len(s.info)*slice/e.slices, len(s.info)*(slice+1)/e.slices
		s.mv.BeginSlice(first)
		w := bitstream.NewWriter()
		e.bSliceHeader(w, first)
		var c cabacWriter
		if e.cabac {
			for w.BitsWritten()%8 != 0 {
				w.WriteBit(1)
			}
			ctx, err := cabac.InitContexts(false, 0, min(51, max(0, e.frameQP)))
			if err != nil {
				return nil, nil, err
			}
			ce, err := cabac.NewEncoder(w)
			if err != nil {
				return nil, nil, err
			}
			c = cabacWriter{e: ce, ctx: ctx}
		}
		skipRun := uint32(0)
		qp := e.frameQP
		for addr := first; addr < last; addr++ {
			i := &s.info[addr]
			i.start = first
			i.inter = true
			for n := range i.modes {
				i.modes[n] = -1
			}
			x, y := addr%src.MBWidth*16, addr/src.MBWidth*16
			s.mv.SetRect(x/4, y/4, 4, 4, motion.MV{}, 0)
			for comp := 0; comp < 3; comp++ {
				n, px, py := 16, x, y
				if comp > 0 {
					n, px, py = 8, x/2, y/2
				}
				pl := &s.pic.Planes[comp]
				for yy := 0; yy < n; yy++ {
					for xx := range n {
						v := prediction.BiWeightSampleHigh(e.bPast.Planes[comp].Sample(px+xx, py+yy), e.bFuture.Planes[comp].Sample(px+xx, py+yy), 1, 1, 0, 0, 0, pl.BitDepth)
						pl.SetSample(px+xx, py+yy, v)
					}
				}
			}
			res, err := s.bResidualRD(addr, qp)
			if err != nil {
				return nil, nil, err
			}
			if !e.cabac {
				if i.skip {
					i.qp = qp
					skipRun++
					continue
				}
				w.WriteUE(skipRun)
				skipRun = 0
				w.WriteUE(0) // B_Direct_16x16无运动差语法。
				if err = s.writeInterResidual(w, addr, &qp, &res); err != nil {
					return nil, nil, err
				}
				continue
			}
			inc := 0
			for _, neighbor := range s.cabacNeighbors(addr) {
				if neighbor >= 0 && !s.info[neighbor].skip {
					inc++
				}
			}
			c.bin(24+inc, flag(i.skip))
			if i.skip {
				i.qp = qp
				c.lastDelta = false
			} else {
				// B_Direct_16x16: 所有已编码邻宏块均为direct，ctxIdxInc=0。
				c.bin(27, 0)
				c.residualMacroblock(s, addr, &qp, &res)
			}
			c.e.Terminate(uint8(flag(addr == last-1)))
			if err = c.e.Err(); err != nil {
				return nil, nil, err
			}
		}
		var payload []byte
		if e.cabac {
			payload = cabacZeroWordPayload(w.Bytes(), c.e.BinCount(), last-first, src.Planes[0].BitDepth)
		} else {
			if skipRun > 0 {
				w.WriteUE(skipRun)
			}
			bitstream.WriteRBSPTrailingBits(w)
			payload = bitstream.RBSPToEBSP(w.Bytes())
		}
		if err = aw.Write(nal.Unit{Header: nal.Header{Type: nal.TypeSlice, RefIDC: 0}, Payload: payload}); err != nil {
			return nil, nil, err
		}
	}
	s.mv.BeginSlice(0)
	s.deblock()
	s.src, s.reference = nil, nil
	return out.Bytes(), s, nil
}

func (e *Encoder) bSliceHeader(w *bitstream.Writer, first int) {
	w.WriteUE(uint32(first))
	w.WriteUE(1)
	w.WriteUE(0)
	w.WriteBits(4, uint64(e.frameNum))
	w.WriteBits(16, uint64(e.currentPOC))
	w.WriteBit(1) // direct_spatial_mv_pred_flag
	w.WriteBit(0) // num_ref_idx_active_override_flag: L0/L1各一个
	w.WriteBit(0)
	w.WriteBit(0) // 两列表默认POC顺序：L0过去锚点、L1未来锚点
	if e.cabac {
		w.WriteUE(0) // cabac_init_idc；非参考B不携带dec_ref_pic_marking。
	}
	w.WriteSE(int32(e.frameQP - e.cfg.QP))
	w.WriteUE(0)
	w.WriteSE(0)
	w.WriteSE(0)
}
