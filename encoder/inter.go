package encoder

import (
	"bytes"
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
	"github.com/goavc/h264/internal/transform"
	"github.com/goavc/h264/nal"
)

// WithSearchRange 配置相对零位移的运动搜索半径（整数像素，1..64）。
func WithSearchRange(pixels int) Option {
	return func(e *Encoder) error {
		if pixels < 1 || pixels > 64 {
			return fmt.Errorf("encoder: search radius %d: %w", pixels, h264.ErrInvalidData)
		}
		e.searchRange = pixels
		return nil
	}
}

// WithFullSearch 使用范围内穷举代替默认菱形搜索；两者都做亚像素细化。
func WithFullSearch() Option { return func(e *Encoder) error { e.fullSearch = true; return nil } }

func (e *Encoder) encodeP(src *picture.Picture) ([]byte, *frameState, error) {
	s, err := e.newFrameState(src)
	if err != nil {
		return nil, nil, err
	}
	s.reference = e.reference
	var b bytes.Buffer
	out := nal.NewAnnexBWriter(&b)
	total := len(s.info)
	for slice := 0; slice < e.slices; slice++ {
		first, last := total*slice/e.slices, total*(slice+1)/e.slices
		s.mv.BeginSlice(first)
		qp := e.cfg.QP
		skipRun := uint32(0)
		w := bitstream.NewWriter()
		e.pSliceHeader(w, first)
		for addr := first; addr < last; addr++ {
			s.info[addr].start = first
			res, err := s.analyzeRD(addr, qp, e.searchRange, e.fullSearch)
			if err != nil {
				return nil, nil, err
			}
			if s.info[addr].skip {
				s.info[addr].qp = qp
				skipRun++
				continue
			}
			w.WriteUE(skipRun)
			skipRun = 0
			if s.info[addr].pcm {
				s.writePCM(w, addr)
				continue
			}
			trial := bitstream.NewWriter()
			previousQP := qp
			if s.info[addr].inter {
				err = s.writeInter(trial, addr, &qp, &res)
			} else {
				err = s.writeMB(trial, addr, &qp, &res)
			}
			if err != nil {
				return nil, nil, err
			}
			if trial.BitsWritten() > 3072 {
				qp = previousQP
				s.info[addr].inter = false
				s.info[addr].skip = false
				s.writePCM(w, addr)
				s.mv.SetRect(addr%src.MBWidth*4, addr/src.MBWidth*4, 4, 4, motion.MV{}, -1)
			} else {
				appendBits(w, trial)
			}
		}
		if skipRun > 0 {
			w.WriteUE(skipRun)
		}
		bitstream.WriteRBSPTrailingBits(w)
		if err = out.Write(nal.Unit{Header: nal.Header{Type: nal.TypeSlice, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(w.Bytes())}); err != nil {
			return nil, nil, err
		}
	}
	s.mv.BeginSlice(0)
	s.deblock()
	s.src = nil
	s.reference = nil
	return b.Bytes(), s, nil
}

func (e *Encoder) pSliceHeader(w *bitstream.Writer, first int) {
	w.WriteUE(uint32(first))
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteBits(4, uint64(e.frameNum))
	w.WriteBit(0) // num_ref_idx_active_override_flag，单参考来自 PPS
	w.WriteBit(0) // ref_pic_list_modification_flag_l0
	w.WriteBit(0) // adaptive_ref_pic_marking_mode_flag，滑窗保留最近一个参考
	w.WriteSE(0)
	w.WriteUE(0)
	w.WriteSE(0)
	w.WriteSE(0)
}

type partition struct{ x, y, w, h int }

var partitions = [4][]partition{
	{{0, 0, 16, 16}}, {{0, 0, 16, 8}, {0, 8, 16, 8}}, {{0, 0, 8, 16}, {8, 0, 8, 16}}, {{0, 0, 8, 8}, {8, 0, 8, 8}, {0, 8, 8, 8}, {8, 8, 8, 8}},
}

func (s *frameState) predictPartition(addr, kind, index int) motion.MV {
	p := partitions[kind][index]
	bx, by := addr%s.pic.MBWidth*4+p.x/4, addr/s.pic.MBWidth*4+p.y/4
	if kind == 1 {
		return motion.Predict16x8(s.mv, bx, by, index == 1, 0)
	}
	if kind == 2 {
		return motion.Predict8x16(s.mv, bx, by, index == 1, 0)
	}
	return motion.Predict(s.mv, bx, by, p.w/4, 0)
}

func (s *frameState) mc(addr int, p partition, mv motion.MV) {
	x, y := addr%s.pic.MBWidth*16+p.x, addr/s.pic.MBWidth*16+p.y
	pl := &s.pic.Planes[0]
	prediction.MCBlockLuma(pl.Data[pl.Index(x, y):], pl.Stride, &s.reference.Planes[0], x, y, p.w, p.h, int(mv.X), int(mv.Y))
	for c := 1; c < 3; c++ {
		pl = &s.pic.Planes[c]
		prediction.MCBlockChroma(pl.Data[pl.Index(x/2, y/2):], pl.Stride, &s.reference.Planes[c], x/2, y/2, p.w/2, p.h/2, int(mv.X), int(mv.Y))
	}
}

func (s *frameState) interResidual(addr int) residual {
	x, y := addr%s.pic.MBWidth*16, addr/s.pic.MBWidth*16
	info := &s.info[addr]
	var res residual
	for r := 0; r < 16; r++ {
		coeff := residualTransform(&s.src.Planes[0], &s.pic.Planes[0], x+r%4*4, y+r/4*4)
		transform.Quant4x4(&res.luma[r], &coeff, info.qp)
		if nonzero(res.luma[r][:]) {
			info.cbp |= 1 << ((r/4/2)*2 + r%4/2)
		}
	}
	cbp := 0
	qpc := transform.ChromaQP(info.qp, 0)
	for c := 0; c < 2; c++ {
		var dc, had [4]int32
		for r := 0; r < 4; r++ {
			coeff := residualTransform(&s.src.Planes[c+1], &s.pic.Planes[c+1], x/2+r%2*4, y/2+r/2*4)
			dc[r] = coeff[0]
			coeff[0] = 0
			transform.Quant4x4(&res.chroma[c][r], &coeff, qpc)
			if nonzero(res.chroma[c][r][:]) {
				cbp = 2
			}
		}
		transform.ForwardChromaDC(&had, &dc)
		transform.QuantDC(res.chromaDC[c][:], had[:], qpc)
		if cbp == 0 && nonzero(res.chromaDC[c][:]) {
			cbp = 1
		}
	}
	info.cbp |= cbp << 4
	return res
}

func (s *frameState) clearMotion(addr int) { // 下一候选的“未来块”不可参与 MVP。
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	s.mv.ClearRect(x, y, 4, 4)
}

func (s *frameState) reconstructInter(addr int, res *residual) {
	x, y := addr%s.pic.MBWidth*16, addr/s.pic.MBWidth*16
	qp := s.info[addr].qp
	for r := 0; r < 16; r++ {
		reconstruct(&s.pic.Planes[0], x+r%4*4, y+r/4*4, &res.luma[r], qp, nil)
	}
	qpc := transform.ChromaQP(qp, 0)
	for c := 0; c < 2; c++ {
		var dc [4]int32
		transform.DequantChromaDC(&dc, &res.chromaDC[c], qpc)
		for r := 0; r < 4; r++ {
			reconstruct(&s.pic.Planes[c+1], x/2+r%2*4, y/2+r/2*4, &res.chroma[c][r], qpc, &dc[r])
		}
	}
}

var interCBP = [48]int{0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13, 14, 6, 9, 31, 35, 37, 42, 44, 33, 34, 36, 40, 39, 43, 45, 46, 17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22, 25, 38, 41}

func (s *frameState) writeInter(w *bitstream.Writer, addr int, qp *int, res *residual) error {
	info := &s.info[addr]
	w.WriteUE(uint32(info.partition))
	if info.partition == 3 {
		for range 4 {
			w.WriteUE(0)
		}
	}
	var actual [4]motion.MV
	x, y := addr%s.pic.MBWidth*4, addr/s.pic.MBWidth*4
	for i, p := range partitions[info.partition] {
		actual[i], _, _ = s.mv.Get(x+p.x/4, y+p.y/4)
	}
	s.clearMotion(addr)
	for i, p := range partitions[info.partition] {
		pred := s.predictPartition(addr, info.partition, i)
		w.WriteSE(actual[i].X - pred.X)
		w.WriteSE(actual[i].Y - pred.Y)
		s.mv.SetRect(x+p.x/4, y+p.y/4, p.w/4, p.h/4, actual[i], 0)
	}
	for code, cbp := range interCBP {
		if cbp == info.cbp {
			w.WriteUE(uint32(code))
			break
		}
	}
	if info.cbp != 0 {
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
	return s.writeResidual(w, addr, res)
}
