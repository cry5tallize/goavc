package encoder

import (
	"bytes"
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/motion/search"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/prediction"
	"github.com/cry5tallize/goavc/internal/simd"
	"github.com/cry5tallize/goavc/internal/transform"
	"github.com/cry5tallize/goavc/nal"
)

type mbInfo struct {
	start, qp, cbp, mode16, chromaMode int
	is16                               bool
	transform8                         bool
	pcm                                bool
	inter, skip                        bool
	partition                          int
	interDeadZone                      bool
	modes                              [16]int8
}
type residual struct {
	luma8    [4][64]int32
	luma     [16][16]int32
	lumaDC   [16]int32
	chroma8  [2][4][64]int32
	chroma   [2][16][16]int32
	chromaDC [2][16]int32
}
type frameState struct {
	src, pic        *picture.Picture
	ownsPic         bool
	storage         *frameStorage
	info            []mbInfo
	nz              []uint8
	cnz             [2][]uint8
	reference       *picture.Picture
	searchReference *picture.Picture
	searchWorkspace *search.Workspace
	trialWriters    *trialWriters
	weights         *PredictionWeights
	allow8          bool
	scaling8        *[6][64]uint8
	scaling4        *[6][16]uint8
	mv              *motion.Grid
	allowZeroPCM    bool
	maxLevelPrefix  int
	dcCoded         [3][]bool
	mvd             []motion.MV
	separate        bool
	independent     bool // 真正的独立色平面；separate也用于共同444的临时亮度视图。
	plane           int
	reuseModes      bool
	field           bool
	forceNoSkip     bool
	fields          [2]*frameState // MBAFF保留两套同奇偶元数据，pic仍指向完整帧。
}

// WithMacroblockQP 使用坐标回调覆盖 CQP；8位为0..51，10位为-12..51。
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

func (e *Encoder) encodeIntra(src *picture.Picture, idr bool) ([]byte, *frameState, error) {
	s, err := e.newFrameState(src)
	if err != nil {
		return nil, nil, err
	}
	success := false
	defer func() {
		if !success {
			releaseFrameState(s)
		}
	}()
	total := src.MBWidth * src.MBHeight
	var b bytes.Buffer
	out := nal.NewAnnexBWriter(&b)
	emit := func(typ nal.UnitType, rb []byte) error {
		return out.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rb)})
	}
	if idr && (!e.separateColourPlane || e.colourPlaneID == 0) {
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
	}
	for slice := 0; slice < e.slices; slice++ {
		start, end := total*slice/e.slices, total*(slice+1)/e.slices
		qp := e.frameQP
		w := bitstream.NewWriter()
		e.sliceHeader(w, start, false, idr)
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
			trial := &s.trialWriters.mb
			trial.Reset()
			previousQP := qp
			if err = s.writeMB(trial, addr, &qp, &res); err != nil {
				return nil, nil, err
			}
			// CQP 的噪声极端情况下残差比原始像素更长；显式选择规范 I_PCM
			// 保证每 MB 的大小上限，Level 自动选择据此给出保守保证。
			if trial.BitsWritten() > uint64(s.rawMBBits()) && s.pcmAllowed(addr) {
				qp = previousQP
				s.writePCM(w, addr)
			} else {
				if err = w.Append(trial); err != nil {
					return nil, nil, err
				}
			}
		}
		bitstream.WriteRBSPTrailingBits(w)
		typ := nal.TypeSlice
		if idr {
			typ = nal.TypeSliceIDR
		}
		if err = emit(typ, w.Bytes()); err != nil {
			return nil, nil, err
		}
	}
	s.deblock()
	s.src = nil
	success = true
	return b.Bytes(), s, nil
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
	// PCM不携带transform_size_8x8_flag；候选回退必须清除原8×8/帧间状态，
	// 否则本地去块会跳过解码器仍执行的4/12像素边，产生参考漂移。
	info.transform8, info.is16, info.inter, info.skip = false, false, false, false
	// PCM的QP'=0；去块元数据保存语法域QPY，10位必须减去位深偏移。
	info.qp = -6 * (s.pic.Planes[0].BitDepth - 8)
	info.cbp = 47
	for i := range info.modes {
		info.modes[i] = 2
		s.nz[addr*16+i] = 16
	}
	for c := range s.cnz {
		for i := range 16 {
			s.cnz[c][addr*16+i] = 16
		}
	}
	w.ByteAlign()
	for c := range s.planeCount() {
		width, height := s.planeMBSize(c)
		x, y := addr%s.pic.MBWidth*width, addr/s.pic.MBWidth*height
		for j := range height {
			for i := range width {
				v := s.src.Planes[c].Sample(x+i, y+j)
				s.pic.Planes[c].SetSample(x+i, y+j, v)
				w.WriteBits(uint(s.pic.Planes[c].BitDepth), uint64(v))
			}
		}
	}
}

// analyzeIntra 用重建邻块做 SATD 搜索；family=1/2 分别为 RD 候选 I4/I16。
// I4 按规范扫描边预测边重建，不影响已经重建的邻宏块或熵上下文。
func (s *frameState) analyzeIntra(addr, family int) residual {
	if family == 3 {
		return s.analyzeIntra8(addr)
	}
	info := &s.info[addr]
	mw := s.pic.MBWidth
	x, y := addr%mw*16, addr/mw*16
	pl := &s.pic.Planes[0]
	var neighbors blockSamplesScratch
	samples, top, left, tl := s.blockSamples(addr, 0, &neighbors)
	var best16 [256]uint16
	mode16 := info.mode16
	if s.reuseModes {
		if family == 2 {
			_ = prediction.Intra16x16High(best16[:], 16, mode16, samples, top, left, pl.BitDepth)
		}
	} else {
		mode16, _ = select16(&best16, &s.src.Planes[0], x, y, samples, top, left, tl)
	}
	var res residual
	if family != 2 {
		for n := 0; n < 16; n++ {
			r := raster(n)
			bx, by := r%4, r/4
			a, t, l, corner := s.samples4(addr, bx, by)
			var pred [16]uint16
			mode := int(info.modes[r])
			if s.reuseModes {
				_ = prediction.Intra4x4High(pred[:], 4, mode, a, t, l, pl.BitDepth)
			} else {
				mode, _ = select4(&pred, &s.src.Planes[0], x+bx*4, y+by*4, a, t, l, corner)
			}
			info.modes[r] = int8(mode)
			putBlock(pl, x+bx*4, y+by*4, 4, pred[:])
			coeff := residualTransform(&s.src.Planes[0], pl, x+bx*4, y+by*4)
			transform.Quant4x4WithScaling(&res.luma[r], &coeff, s.lumaQP(info.qp), s.matrix4(true, 0))
			reconstructScaled(pl, x+bx*4, y+by*4, &res.luma[r], s.lumaQP(info.qp), nil, s.matrix4(true, 0))
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
			transform.Quant4x4WithScaling(&res.luma[r], &coeff, s.lumaQP(info.qp), s.matrix4(true, 0))
		}
		transform.ForwardLumaDC(&had, &dc)
		transform.QuantDCWithScaling(res.lumaDC[:], had[:], s.lumaQP(info.qp), scalingDC(s.matrix4(true, 0)))
		transform.DequantLumaDCWithScaling(&dc, &res.lumaDC, s.lumaQP(info.qp), scalingDC(s.matrix4(true, 0)))
		for r := 0; r < 16; r++ {
			reconstructScaled(pl, x+r%4*4, y+r/4*4, &res.luma[r], s.lumaQP(info.qp), &dc[r], s.matrix4(true, 0))
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
	if s.separate {
		return
	}
	if s.chromaArrayType() == 3 {
		s.chroma444(addr, res, true)
		return
	}
	info := &s.info[addr]
	width, height := s.planeMBSize(1)
	x, y := addr%s.pic.MBWidth*width, addr/s.pic.MBWidth*height
	var best [2][128]uint16
	bestCost := int(^uint(0) >> 1)
	for mode := range 4 {
		var pred [2][128]uint16
		cost, available := 0, true
		for c := range 2 {
			var neighbors blockSamplesScratch
			a, t, l, tl := s.blockSamples(addr, c+1, &neighbors)
			// 可用性与共享预测器的写前校验相同；已知不可用的候选直接跳过，
			// 避免为正常模式枚举构造再丢弃错误，保持原候选顺序和结果。
			if (mode == 1 && !l) || (mode == 2 && !t) || (mode == 3 && !tl) {
				available = false
				break
			}
			predict := prediction.Chroma8x8High
			if height == 16 {
				predict = prediction.Chroma422High
			}
			if predict(pred[c][:], width, mode, a, t, l, s.pic.Planes[c+1].BitDepth) != nil {
				available = false
				break
			}
			for by := 0; by < height; by += 4 {
				for bx := 0; bx < width; bx += 4 {
					var block [16]uint16
					for j := range 4 {
						copy(block[j*4:j*4+4], pred[c][(by+j)*width+bx:])
					}
					cost += blockSATD(&s.src.Planes[c+1], x+bx, y+by, 4, block[:])
				}
			}
		}
		if available && cost < bestCost {
			bestCost, info.chromaMode, best = cost, mode, pred
		}
	}
	for c := range 2 {
		for j := range height {
			for i := range width {
				s.pic.Planes[c+1].SetSample(x+i, y+j, best[c][j*width+i])
			}
		}
	}
	s.subsampledChromaResidual(addr, res, true, true)
}

func residualTransform(src, pred *picture.Plane, x, y int) [16]int32 {
	var residual, coeff [16]int32
	subtractPlanes(residual[:], src, pred, x, y, 4)
	transform.Forward4x4(&coeff, &residual)
	return coeff
}

// subtractPlanes 直接处理原生采样，保留场视图的起点和 stride。
func subtractPlanes(dst []int32, src, pred *picture.Plane, x, y, n int) {
	if src.BitDepth == 10 {
		simd.Subtract16(dst, n, src.Data16[src.Index(x, y):], src.Stride, pred.Data16[pred.Index(x, y):], pred.Stride, n, n)
	} else {
		simd.Subtract8(dst, n, src.Data[src.Index(x, y):], src.Stride, pred.Data[pred.Index(x, y):], pred.Stride, n, n)
	}
}

func reconstruct(pl *picture.Plane, x, y int, levels *[16]int32, qp int, dc *int32) {
	reconstructScaled(pl, x, y, levels, qp, dc, nil)
}

func reconstructScaled(pl *picture.Plane, x, y int, levels *[16]int32, qp int, dc *int32, matrix *[16]uint8) {
	var coeff, out [16]int32
	transform.Dequant4x4WithScaling(&coeff, levels, qp, matrix)
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
func putBlock(p *picture.Plane, x, y, n int, b []uint16) {
	for j := range n {
		for i := range n {
			p.SetSample(x+i, y+j, b[j*n+i])
		}
	}
}
func blockSATD(p *picture.Plane, x, y, n int, pred []uint16) int {
	sum := 0
	for by := 0; by < n; by += 4 {
		for bx := 0; bx < n; bx += 4 {
			var diff [16]int32
			for j := range 4 {
				for i := range 4 {
					diff[j*4+i] = int32(p.Sample(x+bx+i, y+by+j)) - int32(pred[(by+j)*n+bx+i])
				}
			}
			sum += transform.SATD4x4(&diff)
		}
	}
	return sum
}

func select4(dst *[16]uint16, src *picture.Plane, x, y int, a prediction.Intra4x4Samples16, top, left, tl bool) (int, int) {
	best, mode := int(^uint(0)>>1), 2
	for m := 0; m < 9; m++ {
		if !prediction.Intra4x4ModeAvailable(m, top, left, tl) {
			continue
		}
		var p [16]uint16
		prediction.Intra4x4High(p[:], 4, m, a, top, left, src.BitDepth)
		cost := blockSATD(src, x, y, 4, p[:])
		if cost < best {
			best = cost
			mode = m
			*dst = p
		}
	}
	return mode, best
}
func select16(dst *[256]uint16, src *picture.Plane, x, y int, a prediction.IntraBlockSamples16, top, left, tl bool) (int, int) {
	best, mode := int(^uint(0)>>1), 2
	for m := 0; m < 4; m++ {
		if (m == 0 && !top) || (m == 1 && !left) || (m == 3 && !tl) {
			continue
		}
		var p [256]uint16
		if prediction.Intra16x16High(p[:], 16, m, a, top, left, src.BitDepth) != nil {
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
