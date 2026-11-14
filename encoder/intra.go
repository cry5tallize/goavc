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

type mbInfo struct {
	start, qp, cbp, mode16, chromaMode int
	is16                               bool
	pcm                                bool
	inter, skip                        bool
	partition                          int
	modes                              [16]int8
}
type residual struct {
	luma     [16][16]int32
	lumaDC   [16]int32
	chroma   [2][4][16]int32
	chromaDC [2][4]int32
}
type frameState struct {
	src, pic       *picture.Picture
	info           []mbInfo
	nz             []uint8
	cnz            [2][]uint8
	reference      *picture.Picture
	mv             *motion.Grid
	allowZeroPCM   bool
	maxLevelPrefix int
	dcCoded        [3][]bool
	mvd            []motion.MV
}

// WithMacroblockQP 使用坐标回调覆盖 CQP；返回值必须为 0..51。
// 回调在 SendFrame 内同步调用，不得重入编码器。不同 QP 通过 mb_qp_delta 写出。
func WithMacroblockQP(qp func(x, y int) int) Option {
	return func(e *Encoder) error {
		if qp == nil {
			return fmt.Errorf("encoder: nil QP callback: %w", h264.ErrInvalidData)
		}
		e.qpAt = qp
		return nil
	}
}

func (e *Encoder) encodeIntra(src *picture.Picture) ([]byte, *frameState, error) {
	s, err := e.newFrameState(src)
	if err != nil {
		return nil, nil, err
	}
	total := src.MBWidth * src.MBHeight
	var b bytes.Buffer
	out := nal.NewAnnexBWriter(&b)
	emit := func(typ nal.UnitType, rb []byte) error {
		return out.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rb)})
	}
	sr, err := e.sps.MarshalRBSP()
	if err != nil {
		return nil, nil, err
	}
	pr, err := e.pps.MarshalRBSP(e.sps)
	if err != nil {
		return nil, nil, err
	}
	if err = emit(nal.TypeSPS, sr); err != nil {
		return nil, nil, err
	}
	if err = emit(nal.TypePPS, pr); err != nil {
		return nil, nil, err
	}
	for slice := 0; slice < e.slices; slice++ {
		start, end := total*slice/e.slices, total*(slice+1)/e.slices
		qp := e.cfg.QP
		w := bitstream.NewWriter()
		e.sliceHeader(w, start, false)
		for addr := start; addr < end; addr++ {
			s.info[addr].start = start
			res, err := s.analyzeIntraRD(addr, qp)
			if err != nil {
				return nil, nil, err
			}
			if s.info[addr].pcm {
				s.writePCM(w, addr)
				continue
			}
			trial := bitstream.NewWriter()
			previousQP := qp
			if err = s.writeMB(trial, addr, &qp, &res); err != nil {
				return nil, nil, err
			}
			// CQP 的噪声极端情况下残差比原始像素更长；显式选择规范 I_PCM
			// 保证每 MB 的大小上限，Level 自动选择据此给出保守保证。
			if trial.BitsWritten() > 3072 && s.pcmAllowed(addr) {
				qp = previousQP
				s.writePCM(w, addr)
			} else {
				appendBits(w, trial)
			}
		}
		bitstream.WriteRBSPTrailingBits(w)
		if err = emit(nal.TypeSliceIDR, w.Bytes()); err != nil {
			return nil, nil, err
		}
	}
	s.deblock()
	s.src = nil
	return b.Bytes(), s, nil
}

func appendBits(dst, src *bitstream.Writer) {
	remaining := src.BitsWritten()
	for _, v := range src.Bytes() {
		n := uint(min(8, remaining))
		dst.WriteBits(n, uint64(v)>>(8-n))
		remaining -= uint64(n)
	}
}

func (s *frameState) writePCM(w *bitstream.Writer, addr int) {
	typ := uint32(25)
	if s.reference != nil {
		typ += 5
	}
	w.WriteUE(typ)
	s.writePCMSamples(w, addr)
}

func (s *frameState) writePCMSamples(w *bitstream.Writer, addr int) {
	info := &s.info[addr]
	info.pcm = true
	info.qp = 0
	info.cbp = 47
	for i := range info.modes {
		info.modes[i] = 2
		s.nz[addr*16+i] = 16
	}
	for c := range s.cnz {
		for i := 0; i < 4; i++ {
			s.cnz[c][addr*4+i] = 16
		}
	}
	w.ByteAlign()
	for c := 0; c < 3; c++ {
		n := 16
		if c > 0 {
			n = 8
		}
		x, y := addr%s.pic.MBWidth*n, addr/s.pic.MBWidth*n
		for j := 0; j < n; j++ {
			row := s.src.Planes[c].Row(y + j)[x : x+n]
			copy(s.pic.Planes[c].Row(y + j)[x:x+n], row)
			for _, v := range row {
				w.WriteByte(v)
			}
		}
	}
}

// analyzeIntra 用重建邻块做 SATD 搜索；family=1/2 分别为 RD 候选 I4/I16。
// I4 按规范扫描边预测边重建，不影响已经重建的邻宏块或熵上下文。
func (s *frameState) analyzeIntra(addr, family int) residual {
	info := &s.info[addr]
	mw := s.pic.MBWidth
	x, y := addr%mw*16, addr/mw*16
	pl := &s.pic.Planes[0]
	samples, top, left, tl := s.blockSamples(addr, 0)
	var best16 [256]byte
	mode16, _ := select16(&best16, &s.src.Planes[0], x, y, samples, top, left, tl)
	var res residual
	if family != 2 {
		for n := 0; n < 16; n++ {
			r := raster(n)
			bx, by := r%4, r/4
			a, t, l, corner := s.samples4(addr, bx, by)
			var pred [16]byte
			mode, _ := select4(&pred, &s.src.Planes[0], x+bx*4, y+by*4, a, t, l, corner)
			info.modes[r] = int8(mode)
			putBlock(pl, x+bx*4, y+by*4, 4, pred[:])
			coeff := residualTransform(&s.src.Planes[0], pl, x+bx*4, y+by*4)
			transform.Quant4x4(&res.luma[r], &coeff, info.qp)
			reconstruct(pl, x+bx*4, y+by*4, &res.luma[r], info.qp, nil)
		}
	}
	if family == 2 {
		info.is16 = true
		info.mode16 = mode16
		putBlock(pl, x, y, 16, best16[:])
		var dc, had [16]int32
		for r := 0; r < 16; r++ {
			coeff := residualTransform(&s.src.Planes[0], pl, x+r%4*4, y+r/4*4)
			dc[r] = coeff[0]
			coeff[0] = 0
			transform.Quant4x4(&res.luma[r], &coeff, info.qp)
		}
		transform.ForwardLumaDC(&had, &dc)
		transform.QuantDC(res.lumaDC[:], had[:], info.qp)
		transform.DequantLumaDC(&dc, &res.lumaDC, info.qp)
		for r := 0; r < 16; r++ {
			reconstruct(pl, x+r%4*4, y+r/4*4, &res.luma[r], info.qp, &dc[r])
			info.modes[r] = 2
		}
	}
	for r := 0; r < 16; r++ {
		if nonzero(res.luma[r][:]) {
			if info.is16 {
				info.cbp |= 15
			} else {
				info.cbp |= 1 << ((r/4/2)*2 + r%4/2)
			}
		}
	}
	s.chroma(addr, &res)
	return res
}

func (s *frameState) chroma(addr int, res *residual) {
	info := &s.info[addr]
	x, y := addr%s.pic.MBWidth*8, addr/s.pic.MBWidth*8
	var best [2][64]byte
	bestCost := int(^uint(0) >> 1)
	for mode := 0; mode < 4; mode++ {
		var pred [2][64]byte
		cost := 0
		available := true
		for c := 0; c < 2; c++ {
			a, t, l, tl := s.blockSamples(addr, c+1)
			if mode == 3 && !tl {
				available = false
				break
			}
			if prediction.Chroma8x8(pred[c][:], 8, mode, a, t, l) != nil {
				available = false
				break
			}
			cost += blockSATD(&s.src.Planes[c+1], x, y, 8, pred[c][:])
		}
		if available && cost < bestCost {
			bestCost = cost
			info.chromaMode = mode
			best = pred
		}
	}
	cbp := 0
	qpc := transform.ChromaQP(info.qp, 0)
	for c := 0; c < 2; c++ {
		pl := &s.pic.Planes[c+1]
		putBlock(pl, x, y, 8, best[c][:])
		var dc, had [4]int32
		for r := 0; r < 4; r++ {
			coeff := residualTransform(&s.src.Planes[c+1], pl, x+r%2*4, y+r/2*4)
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
		transform.DequantChromaDC(&dc, &res.chromaDC[c], qpc)
		for r := 0; r < 4; r++ {
			reconstruct(pl, x+r%2*4, y+r/2*4, &res.chroma[c][r], qpc, &dc[r])
		}
	}
	info.cbp |= cbp << 4
}

func residualTransform(src, pred *picture.Plane, x, y int) [16]int32 {
	var residual, coeff [16]int32
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			residual[j*4+i] = int32(src.Pix(x+i, y+j)) - int32(pred.Pix(x+i, y+j))
		}
	}
	transform.Forward4x4(&coeff, &residual)
	return coeff
}

func reconstruct(pl *picture.Plane, x, y int, levels *[16]int32, qp int, dc *int32) {
	var coeff, out [16]int32
	transform.Dequant4x4(&coeff, levels, qp)
	if dc != nil {
		coeff[0] = *dc
	}
	transform.IDCT4x4(&out, &coeff)
	picture.AddResidual4x4(pl, x, y, &out)
}

func nonzero(v []int32) bool {
	for _, x := range v {
		if x != 0 {
			return true
		}
	}
	return false
}
func raster(n int) int   { return (n/8*2+n%4/2)*4 + (n/4%2)*2 + n%2 }
func order(x, y int) int { return (y/2*2+x/2)*4 + y%2*2 + x%2 }
func putBlock(p *picture.Plane, x, y, n int, b []byte) {
	for j := 0; j < n; j++ {
		copy(p.Row(y + j)[x:x+n], b[j*n:(j+1)*n])
	}
}
func blockSATD(p *picture.Plane, x, y, n int, pred []byte) int {
	return transform.SATD(p.Data[p.Index(x, y):], p.Stride, pred, n, n, n)
}

func select4(dst *[16]byte, src *picture.Plane, x, y int, a prediction.Intra4x4Samples, top, left, tl bool) (int, int) {
	best, mode := int(^uint(0)>>1), 2
	for m := 0; m < 9; m++ {
		if !prediction.Intra4x4ModeAvailable(m, top, left, tl) {
			continue
		}
		var p [16]byte
		prediction.Intra4x4(p[:], 4, m, a, top, left)
		cost := blockSATD(src, x, y, 4, p[:])
		if cost < best {
			best = cost
			mode = m
			*dst = p
		}
	}
	return mode, best
}
func select16(dst *[256]byte, src *picture.Plane, x, y int, a prediction.IntraBlockSamples, top, left, tl bool) (int, int) {
	best, mode := int(^uint(0)>>1), 2
	for m := 0; m < 4; m++ {
		if m == 3 && !tl {
			continue
		}
		var p [256]byte
		if prediction.Intra16x16(p[:], 16, m, a, top, left) != nil {
			continue
		}
		cost := blockSATD(src, x, y, 16, p[:])
		if cost < best {
			best = cost
			mode = m
			*dst = p
		}
	}
	return mode, best
}
