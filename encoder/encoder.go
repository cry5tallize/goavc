package encoder

import (
	"bytes"
	"encoding/binary"
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// Encoder 是串行8/10-bit 4:2:0、4:2:2和4:4:4编码器，支持双锚点非参考B与显示重排。
// 同一实例非并发安全；输入在SendFrame返回前消费或复制到有界内部队列。
type Encoder struct {
	cfg                       h264.EncoderConfig
	pcm                       bool
	interlaced                bool
	topFieldFirst             bool
	searchRange               int
	fullSearch                bool
	maxAUBytes                int
	adaptiveQP                int
	cabac                     bool
	autoWeights               bool
	fixedWeights              *PredictionWeights
	transform8                bool
	scaling8                  *[6][64]uint8
	scaling4                  *[6][16]uint8
	separateColourPlane       bool
	colourPlaneID             int
	planeReconstruction       *picture.Picture
	lookahead                 int
	pendingFrames             []*h264.Frame
	lookaheadPrevious         *h264.Frame
	bInputs                   []bInput
	bReady                    []bInput
	bPast, bFuture            *picture.Picture
	encodingB                 bool
	bCurrentIndex, bNextIndex int64
	bIDRIndex, bIntraIndex    int64
	currentPOC                uint32
	bClock, bStep             int64
	bInputOrigin              int64
	bLastInputPTS             int64
	bClockSet                 bool
	bPreviousSource           *h264.Frame
	sceneThreshold            int
	frameQP, lastQP           int
	vbvFullness               int64
	lastFrameBits             int64
	vbvBufferBits             int64
	rcRemainder               int64
	rateQP                    [3]int
	rateQPKnown               [3]bool
	reference                 *picture.Picture
	frameNum                  uint32
	framesSinceIDR            int
	framesInGOP               int
	reconKeyFrame             bool
	qpAt                      func(x, y int) int
	last                      *frameState
	slices                    int
	sps                       *syntax.SPS
	pps                       *syntax.PPS
	out                       *h264.Packet
	recon                     *picture.Picture
	reconPTS, reconDuration   int64
	idr                       uint32
	flushed, closed           bool
}

// Option 配置编码通道；选项在构造时校验，不允许静默回退。
type Option func(*Encoder) error

// WithPCM 选择无损 I_PCM 通道，用于传输和互操作检查。
func WithPCM() Option { return func(e *Encoder) error { e.pcm = true; return nil } }

// WithSeparateColourPlanes 将4:4:4的三个分量编码为独立亮度平面。
func WithSeparateColourPlanes() Option {
	return func(e *Encoder) error {
		subW, subH := e.cfg.PixelFormat.ChromaSubsampling()
		if subW != 1 || subH != 1 {
			return fmt.Errorf("encoder: separate colour planes require 444: %w", h264.ErrInvalidData)
		}
		e.separateColourPlane = true
		return nil
	}
}

// WithCABAC 选择CABAC或CAVLC；默认Baseline用CAVLC，其余Profile用CABAC。
// Baseline禁止CABAC，选项在New中统一校验，不改变位深或预测工具。
func WithCABAC(enabled bool) Option {
	return func(e *Encoder) error {
		e.cabac = enabled
		return nil
	}
}

// WithSliceCount 将宏块光栅序列等分为 count 个连续 slice（1..64）。
func WithSliceCount(count int) Option {
	return func(e *Encoder) error {
		if count < 1 || count > 64 {
			return fmt.Errorf("encoder: slice count %d: %w", count, h264.ErrInvalidData)
		}
		e.slices = count
		return nil
	}
}

// WithSceneChangeThreshold 按所有 Y/U/V 采样的平均绝对变化触发 IDR。
// threshold 为归一到 0..100 的样本幅值百分比；零值关闭检测。
func WithSceneChangeThreshold(threshold int) Option {
	return func(e *Encoder) error {
		if threshold < 1 || threshold > 100 {
			return fmt.Errorf("encoder: scene threshold %d: %w", threshold, h264.ErrInvalidData)
		}
		e.sceneThreshold = threshold
		return nil
	}
}

// New 创建8/10-bit 4:2:0、4:2:2或4:4:4串行编码器。Baseline支持I/P，其余Profile可设置
// MaxBFrames启用非参考spatial B_Direct/B_Skip及POC/DTS重排。
func New(cfg h264.EncoderConfig, options ...Option) (*Encoder, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	subW, subH := cfg.PixelFormat.ChromaSubsampling()
	depth := cfg.PixelFormat.BitDepth()
	legal := false
	switch cfg.Profile {
	case h264.ProfileBaseline, h264.ProfileMain, h264.ProfileHigh:
		legal = depth == 8 && subW == 2 && subH == 2
	case h264.ProfileHigh10:
		legal = depth <= 10 && subW == 2 && subH == 2
	case h264.ProfileHigh422:
		legal = depth <= 10 && subW == 2
	case h264.ProfileHigh444:
		legal = depth <= 10
	}
	if !legal || cfg.Threads > 1 {
		return nil, fmt.Errorf("encoder: incompatible profile/format or nonserial configuration: %w", h264.ErrUnsupported)
	}
	// 渐进帧crop单位随色度采样变化；444允许奇数显示尺寸。
	if cfg.Width%subW != 0 || cfg.Height%subH != 0 {
		return nil, fmt.Errorf("encoder: dimensions are not representable by SPS crop units: %w", h264.ErrUnsupported)
	}
	if uint64(cfg.FPSNum) > uint64(^uint32(0))/2 || uint64(cfg.FPSDen) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("encoder: VUI timing overflows: %w", h264.ErrInvalidData)
	}
	e := &Encoder{cfg: cfg, slices: 1, searchRange: 16, cabac: cfg.Profile != h264.ProfileBaseline, frameQP: cfg.QP, lastQP: cfg.QP}
	if cfg.RateControl == h264.RateControlCBR || cfg.RateControl == h264.RateControlVBV {
		e.vbvBufferBits = int64(cfg.VBVBufferSize)
		if e.vbvBufferBits == 0 {
			peak := cfg.MaxBitrate
			if peak == 0 {
				peak = cfg.Bitrate
			}
			e.vbvBufferBits = int64(peak)
		}
		e.vbvFullness = e.vbvBufferBits / 2
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("encoder: nil option: %w", h264.ErrInvalidData)
		}
		if err := option(e); err != nil {
			return nil, err
		}
	}
	if e.interlaced {
		// Annex A：Baseline不允许场编码；帧裁剪的垂直单位为2*SubHeightC。
		if cfg.Profile == h264.ProfileBaseline || cfg.Height%(2*subH) != 0 {
			return nil, fmt.Errorf("encoder: interlaced profile/crop units: %w", h264.ErrUnsupported)
		}
		if e.slices > ((cfg.Width+15)/16)*((cfg.Height+31)/32) {
			return nil, fmt.Errorf("encoder: slice count exceeds macroblock pairs: %w", h264.ErrInvalidData)
		}
	}
	if !e.interlaced && e.slices > ((cfg.Width+15)/16)*((cfg.Height+15)/16) {
		return nil, fmt.Errorf("encoder: slice count %d exceeds macroblocks: %w", e.slices, h264.ErrInvalidData)
	}
	if e.pcm && (e.qpAt != nil || cfg.RateControl != h264.RateControlCQP) {
		return nil, fmt.Errorf("encoder: PCM has no QP override: %w", h264.ErrUnsupported)
	}
	if e.qpAt != nil && cfg.RateControl != h264.RateControlCQP {
		return nil, fmt.Errorf("encoder: explicit macroblock QP conflicts with rate control: %w", h264.ErrUnsupported)
	}
	if e.adaptiveQP > 0 && (e.pcm || e.qpAt != nil) {
		return nil, fmt.Errorf("encoder: AQ conflicts with PCM or explicit QP callback: %w", h264.ErrUnsupported)
	}
	if e.cabac && cfg.Profile == h264.ProfileBaseline {
		return nil, fmt.Errorf("encoder: baseline forbids CABAC: %w", h264.ErrUnsupported)
	}
	if (e.autoWeights || e.fixedWeights != nil) && (cfg.Profile == h264.ProfileBaseline || e.pcm) {
		return nil, fmt.Errorf("encoder: weighted prediction requires Main/High compressed P slices: %w", h264.ErrUnsupported)
	}
	if e.lookahead > 0 && e.pcm {
		return nil, fmt.Errorf("encoder: lookahead is not meaningful for all-PCM IDR: %w", h264.ErrUnsupported)
	}
	if cfg.MaxBFrames > 0 && e.pcm {
		return nil, fmt.Errorf("encoder: B scheduling cannot combine with PCM: %w", h264.ErrUnsupported)
	}
	if err := e.parameters(); err != nil {
		return nil, err
	}
	return e, nil
}

// SendFrame 同步消费输入，返回后调用方可修改或释放输入。
// 输出槽满时返回 ErrAgain 且不消费输入；nil 等价于 Flush。
func (e *Encoder) SendFrame(f *h264.Frame) error {
	if e.closed || e.flushed {
		return h264.ErrEOF
	}
	if f == nil {
		return e.Flush()
	}
	if e.out != nil {
		return h264.ErrAgain
	}
	if err := f.Validate(); err != nil {
		return err
	}
	if f.Width != e.cfg.Width || f.Height != e.cfg.Height || f.Format != e.cfg.PixelFormat {
		return fmt.Errorf("encoder: frame differs from configuration: %w", h264.ErrInvalidData)
	}
	if e.cfg.MaxBFrames > 0 {
		return e.queueBInput(f)
	}
	if e.lookahead > 0 {
		return e.queueLookahead(f)
	}
	return e.encodeFrameNow(f, false)
}

func (e *Encoder) encodeFrameNow(f *h264.Frame, lookaheadScene bool) error {
	if e.pcm && (e.cfg.Profile == h264.ProfileBaseline || e.cfg.Profile == h264.ProfileMain) {
		for comp := 0; comp < 3; comp++ {
			w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
			for y := 0; y < h; y++ {
				for _, v := range f.Planes[comp].Data[y*f.Planes[comp].Stride : y*f.Planes[comp].Stride+w] {
					if v == 0 {
						return fmt.Errorf("encoder: Baseline/Main PCM forbids zero samples: %w", h264.ErrUnsupported)
					}
				}
			}
		}
	}
	mbHeight := (f.Height + 15) / 16
	if e.interlaced {
		mbHeight = (f.Height + 31) / 32 * 2
	}
	pic, err := picture.New((f.Width+15)/16, mbHeight, f.Format)
	if err != nil {
		return err
	}
	for c := range pic.Planes {
		p := &pic.Planes[c]
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		for y := 0; y < p.Height; y++ {
			sourceY := min(y, h-1)
			if e.interlaced && y >= h {
				sourceY = h - 2 + y%2
			}
			src := f.Planes[c].Data[sourceY*f.Planes[c].Stride:]
			if p.BitDepth > 8 {
				row := p.Row16(y)
				for x := range w {
					row[x] = binary.LittleEndian.Uint16(src[x*2:])
				}
				for x := w; x < p.Width; x++ {
					row[x] = row[w-1]
				}
			} else {
				row := p.Row(y)
				copy(row[:w], src[:w])
				for x := w; x < p.Width; x++ {
					row[x] = row[w-1]
				}
			}
		}
	}
	sceneChange := lookaheadScene
	if e.lookahead == 0 && e.cfg.MaxBFrames == 0 {
		sceneChange = e.sceneChanged(pic)
	}
	isIDR := e.pcm || e.reference == nil || e.framesSinceIDR >= e.cfg.IDRInterval || f.KeyFrame || sceneChange
	isIntra := isIDR || e.framesInGOP >= e.cfg.GOPSize
	if e.cfg.MaxBFrames > 0 {
		isIDR = e.reference == nil || e.bCurrentIndex-e.bIDRIndex >= int64(e.cfg.IDRInterval) || f.KeyFrame || lookaheadScene
		isIntra = isIDR || e.bCurrentIndex-e.bIntraIndex >= int64(e.cfg.GOPSize)
		if e.encodingB {
			isIDR, isIntra = false, false
		}
		base := e.bIDRIndex
		if isIDR {
			base = e.bCurrentIndex
		}
		e.currentPOC = uint32((e.bCurrentIndex-base)*2) & 65535
	}
	data, state, selectedQP, err := e.encodeRateControlled(pic, isIntra, isIDR)
	if err != nil {
		return err
	}
	if state != nil {
		pic = state.pic
	}
	if len(data) > e.maxAUBytes {
		return fmt.Errorf("encoder: access unit %d exceeds level budget %d: %w", len(data), e.maxAUBytes, h264.ErrResourceLimit)
	}
	e.recon = pic
	e.frameQP, e.lastQP = selectedQP, selectedQP
	e.commitRateControl(len(data))
	if e.cfg.RateControl != h264.RateControlCQP {
		kind := e.ratePictureKind(isIntra)
		e.rateQP[kind], e.rateQPKnown[kind] = e.frameQP, true
	}
	e.reconPTS, e.reconDuration = f.PTS, f.Duration
	e.last = state
	e.reconKeyFrame = isIDR
	e.out = &h264.Packet{Data: data, PTS: f.PTS, DTS: f.PTS, Duration: f.Duration, KeyFrame: isIDR}
	if e.cfg.MaxBFrames > 0 {
		e.out.DTS = e.bClock
		e.bClock += e.bStep
		if isIDR {
			e.bIDRIndex = e.bCurrentIndex
		}
		if isIntra {
			e.bIntraIndex = e.bCurrentIndex
		}
	}
	if e.encodingB {
		return nil
	}
	if isIDR {
		e.idr = (e.idr + 1) & 65535
		e.frameNum = 1
		e.framesSinceIDR = 1
	} else {
		e.frameNum = (e.frameNum + 1) & 15
		e.framesSinceIDR++
	}
	if isIntra {
		e.framesInGOP = 1
	} else {
		e.framesInGOP++
	}
	if !e.pcm {
		e.reference = pic
	}
	return nil
}

// ReceivePacket 转交一个完整 Annex-B AU；Data 的所有权归调用方。
func (e *Encoder) ReceivePacket() (h264.Packet, error) {
	if e.out == nil && !e.closed && e.cfg.MaxBFrames > 0 && (len(e.bReady) > 0 || e.flushed && len(e.bInputs) > 0) {
		if err := e.encodeNextBOutput(); err != nil {
			return h264.Packet{}, err
		}
	}
	if e.out == nil && !e.closed && e.cfg.MaxBFrames == 0 && e.lookahead > 0 && (len(e.pendingFrames) > e.lookahead || e.flushed && len(e.pendingFrames) > 0) {
		if err := e.encodeLookaheadHead(); err != nil {
			return h264.Packet{}, err
		}
	}
	if e.out != nil {
		p := *e.out
		e.out = nil
		return p, nil
	}
	if e.closed || e.flushed {
		return h264.Packet{}, h264.ErrEOF
	}
	return h264.Packet{}, h264.ErrAgain
}

// Reconstruction 返回最近一次成功 SendFrame 的本地重建快照，归调用方所有。
// 它不消耗输出槽；尚未编码时返回 ErrAgain，关闭后返回 ErrEOF。
func (e *Encoder) Reconstruction() (*h264.Frame, error) {
	if e.closed {
		return nil, h264.ErrEOF
	}
	if e.recon == nil {
		return nil, h264.ErrAgain
	}
	f := &h264.Frame{Width: e.cfg.Width, Height: e.cfg.Height, Format: e.cfg.PixelFormat, KeyFrame: e.reconKeyFrame, PTS: e.reconPTS, Duration: e.reconDuration}
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		bytesPerSample := (e.recon.Planes[c].BitDepth + 7) / 8
		stride := w * bytesPerSample
		f.Planes[c] = h264.Plane{Data: make([]byte, stride*h), Stride: stride}
		for y := 0; y < h; y++ {
			if bytesPerSample == 2 {
				for x, v := range e.recon.Planes[c].Row16(y)[:w] {
					binary.LittleEndian.PutUint16(f.Planes[c].Data[y*stride+x*2:], v)
				}
			} else {
				copy(f.Planes[c].Data[y*stride:], e.recon.Planes[c].Row(y)[:w])
			}
		}
	}
	return f, nil
}

// Flush 结束输入；待取的最后一个包仍可接收。重复调用无副作用。
func (e *Encoder) Flush() error {
	if e.closed {
		return h264.ErrEOF
	}
	e.flushed = true
	return nil
}

// Reset 丢弃输出与重建，保持配置并重新从 IDR 开始。
func (e *Encoder) Reset() error {
	if e.closed {
		return h264.ErrEOF
	}
	e.out = nil
	e.recon = nil
	e.last = nil
	e.reference = nil
	e.releaseLookahead()
	e.releaseBScheduler()
	e.frameNum = 0
	e.framesSinceIDR = 0
	e.framesInGOP = 0
	e.idr = 0
	e.frameQP, e.lastQP = e.cfg.QP, e.cfg.QP
	e.lastFrameBits = 0
	e.rcRemainder = 0
	e.rateQP, e.rateQPKnown = [3]int{}, [3]bool{}
	if e.cfg.RateControl == h264.RateControlCBR || e.cfg.RateControl == h264.RateControlVBV {
		e.vbvFullness = e.vbvBufferBits / 2
	}
	e.flushed = false
	return nil
}

// Close 幂等释放实例资源。
func (e *Encoder) Close() error {
	e.out = nil
	e.recon = nil
	e.last = nil
	e.reference = nil
	e.releaseLookahead()
	e.releaseBScheduler()
	e.closed = true
	return nil
}

func (e *Encoder) encodePCM(pic *picture.Picture) ([]byte, error) {
	var b bytes.Buffer
	out := nal.NewAnnexBWriter(&b)
	appendNAL := func(typ nal.UnitType, rbsp []byte) error {
		return out.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)})
	}
	if !e.separateColourPlane || e.colourPlaneID == 0 {
		s, err := e.sps.MarshalRBSP()
		if err != nil {
			return nil, err
		}
		p, err := e.pps.MarshalRBSP(e.sps)
		if err != nil {
			return nil, err
		}
		if err = appendNAL(nal.TypeSPS, s); err != nil {
			return nil, err
		}
		if err = appendNAL(nal.TypePPS, p); err != nil {
			return nil, err
		}
	}
	total := pic.MBWidth * pic.MBHeight
	for slice := 0; slice < e.slices; slice++ {
		start, end := total*slice/e.slices, total*(slice+1)/e.slices
		w := bitstream.NewWriter()
		e.sliceHeader(w, start, true, true)
		for addr := start; addr < end; addr++ {
			w.WriteUE(25)
			w.ByteAlign()
			planes := 3
			if e.separateColourPlane {
				planes = 1
			}
			for c := range planes {
				mw, mh := pic.Format.PlaneDimensions(c, 16, 16)
				for y := range mh {
					for x := range mw {
						v := pic.Planes[c].Sample(addr%pic.MBWidth*mw+x, addr/pic.MBWidth*mh+y)
						w.WriteBits(uint(pic.Planes[c].BitDepth), uint64(v))
					}
				}
			}
		}
		bitstream.WriteRBSPTrailingBits(w)
		if err := appendNAL(nal.TypeSliceIDR, w.Bytes()); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// 参数均已在 New 中限界；固定宽度和 ue/se 写入不会失败。
func (e *Encoder) sliceHeader(w *bitstream.Writer, first int, disableFilter, idr bool) {
	w.WriteUE(uint32(first))
	w.WriteUE(2)
	w.WriteUE(0)
	if e.separateColourPlane {
		w.WriteBits(2, uint64(e.colourPlaneID))
	}
	frameNum := e.frameNum
	if idr {
		frameNum = 0
	}
	w.WriteBits(4, uint64(frameNum))
	if e.interlaced {
		w.WriteBit(0) // field_pic_flag=0：MBAFF帧，而不是PAFF场图像。
	}
	if idr {
		w.WriteUE(e.idr)
	}
	if e.cfg.MaxBFrames > 0 {
		w.WriteBits(16, uint64(e.currentPOC))
	}
	if idr {
		w.WriteBits(2, 0)
	} else {
		w.WriteBit(0)
	}
	w.WriteSE(int32(e.sliceQPDelta()))
	if disableFilter {
		w.WriteUE(1)
	} else {
		w.WriteUE(0)
		w.WriteSE(0)
		w.WriteSE(0)
	}
}

func (e *Encoder) sliceQPDelta() int {
	// SliceQPY是PPS基准加有符号差，不能沿用mb_qp_delta的模52规则。
	return e.frameQP - e.cfg.QP
}
