// Package decoder 实现 H.264 渐进式 4:2:0/4:2:2 8/10-bit 解码流水线。
//
// 输入单位是访问单元（Packet.Data 为 Annex-B），按显示顺序输出原生格式
// Frame。支持 CAVLC/CABAC 的 I/P/B、多参考/MMCO、direct、加权预测、
// High 8x8/ScalingMatrix，并按色度采样格式处理预测、残差、去块和裁剪。
package decoder

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/dpb"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// Decoder 是单实例解码器。与公共契约一致：实例非并发安全。
type Decoder struct {
	cfg h264.DecoderConfig

	sps map[uint32]*syntax.SPS
	pps map[uint32]*syntax.PPS

	pool *picture.Pool
	dpb  *dpb.DPB
	poc  *dpb.POCState

	out            []*h264.Frame
	pending        []orderedFrame
	hasEmitted     bool
	lastEmittedPOC int32

	// waitingForIDR 在致命错误后置位；后续包只有出现 IDR 才会重新解码。
	waitingForIDR bool
	flushed       bool

	// 序列参数缓存，DPB/POC 在 SPS 变化时重建。
	activeSPS   *syntax.SPS
	maxFrameNum uint32

	closed bool
}

// New 创建解码器并校验配置。
func New(cfg h264.DecoderConfig) (*Decoder, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if (cfg.OutputFormat != h264.YUV420P && cfg.OutputFormat != h264.YUV420P10LE && cfg.OutputFormat != h264.YUV422P && cfg.OutputFormat != h264.YUV422P10LE && cfg.OutputFormat != h264.YUV444P && cfg.OutputFormat != h264.YUV444P10LE) || cfg.Threads > 1 {
		return nil, fmt.Errorf("decoder: only serial 8/10-bit planar YUV output is supported: %w", h264.ErrUnsupported)
	}
	d := &Decoder{
		cfg:  cfg,
		sps:  make(map[uint32]*syntax.SPS),
		pps:  make(map[uint32]*syntax.PPS),
		pool: picture.NewPool(4),
	}
	return d, nil
}

// SendPacket 送入一个访问单元（Annex-B）。
//
// 返回 nil 表示输入已被消费，调用方应继续调用 ReceiveFrame 取输出。
// 语法错误返回 ErrInvalidData；合法但未实现的工具返回 ErrUnsupported；
// 两者都会让解码器进入等待 IDR 状态。
func (d *Decoder) SendPacket(p h264.Packet) error {
	if d.closed || d.flushed {
		return h264.ErrEOF
	}
	if len(p.Data) == 0 {
		return nil
	}
	scanner := nal.NewAnnexBScannerWithLimit(bytes.NewReader(p.Data), d.cfg.MaxNALSize)
	var entries []sliceEntry
	seen := false
	finish := func() error {
		if len(entries) == 0 {
			return nil
		}
		err := d.decodeFrame(entries, p)
		entries = entries[:0]
		return err
	}
	for {
		u, err := scanner.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return d.fail(err)
		}
		seen = true
		if !u.Header.IsVCL() && len(entries) > 0 {
			if err := finish(); err != nil {
				return d.fail(err)
			}
		}
		switch u.Header.Type {
		case nal.TypeSPS:
			rbsp, err := bitstream.EBSPToRBSP(u.Payload)
			if err != nil {
				return d.fail(fmt.Errorf("decoder: sps rbsp: %w", err))
			}
			s, err := syntax.ParseSPS(rbsp)
			if err != nil {
				return d.fail(err)
			}
			if err := d.checkSPSLimits(s); err != nil {
				return d.fail(err)
			}
			d.sps[s.SeqParameterSetID] = s
		case nal.TypePPS:
			rbsp, err := bitstream.EBSPToRBSP(u.Payload)
			if err != nil {
				return d.fail(fmt.Errorf("decoder: pps rbsp: %w", err))
			}
			// 参数集按码流顺序生效，避免同 ID 的后续 SPS/PPS 覆盖前一帧。
			_, spsID, err := peekPPSIDs(rbsp)
			if err != nil {
				return d.fail(fmt.Errorf("decoder: pps header: %w", err))
			}
			s := d.sps[spsID]
			if s == nil {
				return d.fail(fmt.Errorf("decoder: pps references unknown sps %d: %w",
					spsID, h264.ErrInvalidData))
			}
			pp, err := syntax.ParsePPS(rbsp, s)
			if err != nil {
				return d.fail(err)
			}
			d.pps[pp.PicParameterSetID] = pp
		case nal.TypeSEI, nal.TypeAUD, nal.TypeEndOfSequence, nal.TypeEndOfStream:
			// M2 忽略 SEI/AUD/EOS；它们不影响重建。
		case nal.TypeSlicePartitionB, nal.TypeSlicePartitionC:
			if d.waitingForIDR {
				continue
			}
			rbsp, err := bitstream.EBSPToRBSP(u.Payload)
			if err != nil {
				return d.fail(err)
			}
			id, err := bitstream.NewReader(rbsp).ReadUE()
			if err != nil {
				return d.fail(err)
			}
			found := -1
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].partition != nil && entries[i].partition.context.SliceID == id {
					found = i
					break
				}
			}
			if found < 0 {
				return d.fail(fmt.Errorf("decoder: orphan data partition: %w", h264.ErrInvalidData))
			}
			if err := attachPartition(&entries[found], u); err != nil {
				return d.fail(err)
			}
		default:
			if u.Header.Type != nal.TypeSlice && u.Header.Type != nal.TypeSliceIDR && u.Header.Type != nal.TypeSlicePartitionA {
				if err := u.Header.ValidateSupported(); err != nil {
					return d.fail(err)
				}
				continue
			}
			if d.waitingForIDR && !u.Header.IsIDR() {
				continue
			}
			entry, err := d.parseSlice(u)
			if err != nil {
				return d.fail(err)
			}
			if len(entries) > 0 && !samePicture(entries[0], entry) {
				if err := finish(); err != nil {
					return d.fail(err)
				}
			}
			if entry.partition != nil {
				for _, previous := range entries {
					if previous.partition != nil && previous.hdr.ColourPlaneID == entry.hdr.ColourPlaneID && previous.hdr.RedundantPicCnt == entry.hdr.RedundantPicCnt && previous.partition.context.SliceID == entry.partition.context.SliceID {
						return d.fail(fmt.Errorf("decoder: duplicate partition A slice_id: %w", h264.ErrInvalidData))
					}
				}
			}
			if len(entries) >= d.cfg.MaxSliceCount {
				return d.fail(fmt.Errorf("decoder: slice count exceeds %d: %w", d.cfg.MaxSliceCount, h264.ErrResourceLimit))
			}
			entries = append(entries, entry)
		}
	}
	if !seen {
		return d.fail(fmt.Errorf("decoder: missing Annex-B NAL: %w", h264.ErrInvalidData))
	}
	if err := finish(); err != nil {
		return d.fail(err)
	}
	return nil
}

// ReceiveFrame 取出一帧。没有输出时返回 ErrAgain；Flush 后输出排空返回 io.EOF。
func (d *Decoder) ReceiveFrame() (*h264.Frame, error) {
	if d.closed {
		return nil, h264.ErrEOF
	}
	if len(d.out) > 0 {
		f := d.out[0]
		d.out[0] = nil
		d.out = d.out[1:]
		return f, nil
	}
	if d.flushed {
		return nil, h264.ErrEOF
	}
	return nil, h264.ErrAgain
}

// Flush 表示输入结束，按显示顺序排空滞留帧；输出耗尽后 ReceiveFrame 返回 EOF。
func (d *Decoder) Flush() error {
	if d.closed {
		return h264.ErrEOF
	}
	d.drainPending()
	d.flushed = true
	return nil
}

// Reset 清空参数集、DPB 与输出队列，回到初始状态。
func (d *Decoder) Reset() error {
	if d.closed {
		return h264.ErrEOF
	}
	for _, f := range d.out {
		f.Release()
	}
	d.out = nil
	d.discardPending()
	d.hasEmitted = false
	if d.dpb != nil {
		d.dpb.Reset()
	}
	d.sps = make(map[uint32]*syntax.SPS)
	d.pps = make(map[uint32]*syntax.PPS)
	d.activeSPS = nil
	d.poc = nil
	d.waitingForIDR = false
	d.flushed = false
	return nil
}

// Close 释放所有内部缓冲。Close 后解码器不可再用。
func (d *Decoder) Close() error {
	if d.closed {
		return nil
	}
	_ = d.Reset()
	d.pool.Trim()
	d.closed = true
	return nil
}

func (d *Decoder) fail(err error) error {
	if h264.IsFatal(err) {
		d.discardPending()
		d.hasEmitted = false
		d.waitingForIDR = true
		if d.dpb != nil {
			d.dpb.Reset()
		}
		if d.poc != nil {
			d.poc.Reset()
		}
	}
	return err
}

func (d *Decoder) activateSPS(s *syntax.SPS) error {
	if d.activeSPS == s && d.dpb != nil {
		return nil
	}
	if d.activeSPS != nil && d.dpb != nil && reflect.DeepEqual(d.activeSPS, s) {
		d.activeSPS = s
		return nil
	}
	d.activeSPS = s
	d.maxFrameNum = uint32(1) << (s.Log2MaxFrameNumMinus4 + 4)
	if d.dpb != nil {
		d.dpb.Reset()
	}
	d.dpb = dpb.New(d.maxFrameNum, s.GapsInFrameNumValueAllowedFlag)
	if err := d.dpb.SetCapacity(max(1, int(s.MaxNumRefFrames))); err != nil {
		return err
	}
	maxLsb := int32(1) << (s.Log2MaxPicOrderCntLsbMinus4 + 4)
	d.poc = dpb.NewPOCState(maxLsb)
	return nil
}

// checkSPSLimits 在解析后立即校验资源上限，避免后续按恶意尺寸分配。
func (d *Decoder) checkSPSLimits(s *syntax.SPS) error {
	if !s.FrameMbsOnlyFlag && !s.MbAdaptiveFrameFieldFlag {
		return fmt.Errorf("decoder: PAFF field pictures: %w", h264.ErrUnsupported)
	}
	w, h := uint64(s.PicWidthInMbs())*16, uint64(s.FrameHeightInMbs())*16
	dw, err := s.DisplayWidth()
	if err != nil {
		return err
	}
	dh, err := s.DisplayHeight()
	if err != nil {
		return err
	}
	if w == 0 || h == 0 || dw > uint64(d.cfg.MaxWidth) || dh > uint64(d.cfg.MaxHeight) || w > uint64(h264.MaxFrameWidth) || h > uint64(h264.MaxFrameHeight) {
		return fmt.Errorf("decoder: %dx%d exceeds configured limit %dx%d: %w",
			w, h, d.cfg.MaxWidth, d.cfg.MaxHeight, h264.ErrResourceLimit)
	}
	// 按编码尺寸的实际平面采样数与各分量字节宽度计费，不能用420估算422。
	subW, subH := uint64(2), uint64(2)
	if s.ChromaFormatIDC == 2 {
		subH = 1
	} else if s.ChromaFormatIDC == 3 {
		subW, subH = 1, 1
	}
	lumaBytes, chromaBytes := uint64(1), uint64(1)
	if s.BitDepthLumaMinus8 > 0 {
		lumaBytes = 2
	}
	if s.BitDepthChromaMinus8 > 0 {
		chromaBytes = 2
	}
	frameBytes := w * h * lumaBytes
	if s.ChromaFormatIDC != 0 {
		frameBytes += 2 * (w / subW) * (h / subH) * chromaBytes
	}
	if d.cfg.MaxFrameSize > 0 && frameBytes > uint64(d.cfg.MaxFrameSize) {
		return fmt.Errorf("decoder: frame size %d exceeds limit %d: %w",
			frameBytes, d.cfg.MaxFrameSize, h264.ErrResourceLimit)
	}
	if s.MaxNumRefFrames > uint32(d.cfg.MaxDPBFrames) {
		return fmt.Errorf("decoder: DPB limit %d: %w", d.cfg.MaxDPBFrames, h264.ErrResourceLimit)
	}
	if s.VUI != nil && s.VUI.BitstreamRestrictionFlag {
		if s.VUI.MaxNumReorderFrames > s.VUI.MaxDecFrameBuffering {
			return fmt.Errorf("decoder: reorder count exceeds buffer declaration: %w", h264.ErrInvalidData)
		}
		if s.VUI.MaxNumReorderFrames > uint32(d.cfg.MaxDPBFrames) || s.VUI.MaxDecFrameBuffering > uint32(d.cfg.MaxDPBFrames) {
			return fmt.Errorf("decoder: VUI DPB exceeds configured limit: %w", h264.ErrResourceLimit)
		}
	}
	return nil
}

// peekPPSIDs 只读取 PPS 的前两个 ue(v) 字段，用于在完整解析前定位 SPS。
//
// 不直接调用 syntax.ParsePPS 是因为它需要 SPS 上下文（决定 scaling list
// 数量），而这两个字段本身与 SPS 无关。
func peekPPSIDs(rbsp []byte) (ppsID, spsID uint32, err error) {
	r := bitstream.NewReader(rbsp)
	if ppsID, err = r.ReadUE(); err != nil {
		return 0, 0, err
	}
	if spsID, err = r.ReadUE(); err != nil {
		return 0, 0, err
	}
	return ppsID, spsID, nil
}
