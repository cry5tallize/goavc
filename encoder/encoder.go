package encoder

import (
	"bytes"
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// Encoder 是串行 Baseline I/P 编码器，单参考、无显示重排。
// 同一实例非并发安全；输入在 SendFrame 返回前消费完毕。
type Encoder struct {
	cfg                     h264.EncoderConfig
	pcm                     bool
	searchRange             int
	fullSearch              bool
	maxAUBytes              int
	adaptiveQP              int
	reference               *picture.Picture
	frameNum                uint32
	framesSinceIDR          int
	reconKeyFrame           bool
	qpAt                    func(x, y int) int
	last                    *frameState
	slices                  int
	sps                     *syntax.SPS
	pps                     *syntax.PPS
	out                     *h264.Packet
	recon                   *picture.Picture
	reconPTS, reconDuration int64
	idr                     uint32
	flushed, closed         bool
}

// Option 配置编码通道；选项在构造时校验，不允许静默回退。
type Option func(*Encoder) error

// WithPCM 选择无损 I_PCM 通道，用于传输和互操作检查。
func WithPCM() Option { return func(e *Encoder) error { e.pcm = true; return nil } }

// WithSliceCount 将宏块光栅序列等分为 count 个连续 slice（1..64）。
func WithSliceCount(count int) Option {
	return func(e *Encoder) error {
		if count < 1 || count > 64 || count > ((e.cfg.Width+15)/16)*((e.cfg.Height+15)/16) {
			return fmt.Errorf("encoder: slice count %d: %w", count, h264.ErrInvalidData)
		}
		e.slices = count
		return nil
	}
}

// New 创建 Baseline、8-bit 4:2:0、CQP 串行编码器。首帧与 IDRInterval 边界
// 输出 IDR，其余输出单参考 P 帧；GOPSize=1 保留全帧内通道。
func New(cfg h264.EncoderConfig, options ...Option) (*Encoder, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Profile != h264.ProfileBaseline || cfg.PixelFormat != h264.YUV420P || cfg.RateControl != h264.RateControlCQP || cfg.MaxBFrames != 0 || cfg.Threads > 1 {
		return nil, fmt.Errorf("encoder: only serial baseline yuv420p CQP is supported: %w", h264.ErrUnsupported)
	}
	if cfg.IDRInterval != cfg.GOPSize {
		return nil, fmt.Errorf("encoder: independent GOP/IDR intervals are pending: %w", h264.ErrUnsupported)
	}
	// Spec: 7.4.2.1.1，4:2:0 渐进帧的 crop 单位为 2×2，不能无损表达奇数显示尺寸。
	if cfg.Width%2 != 0 || cfg.Height%2 != 0 {
		return nil, fmt.Errorf("encoder: 420 SPS crop requires even dimensions: %w", h264.ErrUnsupported)
	}
	if uint64(cfg.FPSNum) > uint64(^uint32(0))/2 || uint64(cfg.FPSDen) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("encoder: VUI timing overflows: %w", h264.ErrInvalidData)
	}
	e := &Encoder{cfg: cfg, slices: 1, searchRange: 16}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("encoder: nil option: %w", h264.ErrInvalidData)
		}
		if err := option(e); err != nil {
			return nil, err
		}
	}
	if e.pcm && e.qpAt != nil {
		return nil, fmt.Errorf("encoder: PCM has no QP override: %w", h264.ErrUnsupported)
	}
	if e.adaptiveQP > 0 && (e.pcm || e.qpAt != nil) {
		return nil, fmt.Errorf("encoder: AQ conflicts with PCM or explicit QP callback: %w", h264.ErrUnsupported)
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
	pic, err := picture.New((f.Width+15)/16, (f.Height+15)/16)
	if err != nil {
		return err
	}
	for c := range pic.Planes {
		p := &pic.Planes[c]
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		for y := 0; y < p.Height; y++ {
			row := p.Row(y)
			src := f.Planes[c].Data[min(y, h-1)*f.Planes[c].Stride:]
			copy(row[:w], src[:w])
			for x := w; x < p.Width; x++ {
				row[x] = row[w-1]
			}
		}
	}
	var data []byte
	var state *frameState
	isIDR := e.pcm || e.reference == nil || e.framesSinceIDR >= e.cfg.IDRInterval || f.KeyFrame
	if e.pcm {
		data, err = e.encodePCM(pic)
	} else if !isIDR {
		data, state, err = e.encodeP(pic)
		if state != nil {
			pic = state.pic
		}
	} else {
		data, state, err = e.encodeIntra(pic)
		if state != nil {
			pic = state.pic
		}
	}
	if err != nil {
		return err
	}
	if len(data) > e.maxAUBytes {
		return fmt.Errorf("encoder: access unit %d exceeds level budget %d: %w", len(data), e.maxAUBytes, h264.ErrResourceLimit)
	}
	e.recon = pic
	e.reconPTS, e.reconDuration = f.PTS, f.Duration
	e.last = state
	e.reconKeyFrame = isIDR
	e.out = &h264.Packet{Data: data, PTS: f.PTS, DTS: f.PTS, Duration: f.Duration, KeyFrame: isIDR}
	if isIDR {
		e.idr = (e.idr + 1) & 65535
		e.frameNum = 1
		e.framesSinceIDR = 1
	} else {
		e.frameNum = (e.frameNum + 1) & 15
		e.framesSinceIDR++
	}
	if !e.pcm {
		e.reference = pic
	}
	return nil
}

// ReceivePacket 转交一个完整 Annex-B AU；Data 的所有权归调用方。
func (e *Encoder) ReceivePacket() (h264.Packet, error) {
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
	f := &h264.Frame{Width: e.cfg.Width, Height: e.cfg.Height, Format: h264.YUV420P, KeyFrame: e.reconKeyFrame, PTS: e.reconPTS, Duration: e.reconDuration}
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, f.Width, f.Height)
		f.Planes[c] = h264.Plane{Data: make([]byte, w*h), Stride: w}
		for y := 0; y < h; y++ {
			copy(f.Planes[c].Data[y*w:], e.recon.Planes[c].Row(y)[:w])
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
	e.frameNum = 0
	e.framesSinceIDR = 0
	e.idr = 0
	e.flushed = false
	return nil
}

// Close 幂等释放实例资源。
func (e *Encoder) Close() error {
	e.out = nil
	e.recon = nil
	e.last = nil
	e.reference = nil
	e.closed = true
	return nil
}

func (e *Encoder) encodePCM(pic *picture.Picture) ([]byte, error) {
	var b bytes.Buffer
	out := nal.NewAnnexBWriter(&b)
	appendNAL := func(typ nal.UnitType, rbsp []byte) error {
		return out.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)})
	}
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
	total := pic.MBWidth * pic.MBHeight
	for slice := 0; slice < e.slices; slice++ {
		start, end := total*slice/e.slices, total*(slice+1)/e.slices
		w := bitstream.NewWriter()
		e.sliceHeader(w, start, true)
		for addr := start; addr < end; addr++ {
			w.WriteUE(25)
			w.ByteAlign()
			for c := range pic.Planes {
				n := 16
				if c > 0 {
					n = 8
				}
				for y := 0; y < n; y++ {
					for _, v := range pic.Planes[c].Row(addr/pic.MBWidth*n + y)[addr%pic.MBWidth*n : addr%pic.MBWidth*n+n] {
						w.WriteByte(v)
					}
				}
			}
		}
		bitstream.WriteRBSPTrailingBits(w)
		if err = appendNAL(nal.TypeSliceIDR, w.Bytes()); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// 参数均已在 New 中限界；固定宽度和 ue/se 写入不会失败。
func (e *Encoder) sliceHeader(w *bitstream.Writer, first int, disableFilter bool) {
	w.WriteUE(uint32(first))
	w.WriteUE(2)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteUE(e.idr)
	w.WriteBits(2, 0)
	w.WriteSE(0)
	if disableFilter {
		w.WriteUE(1)
	} else {
		w.WriteUE(0)
		w.WriteSE(0)
		w.WriteSE(0)
	}
}
