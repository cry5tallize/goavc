package stream

import (
	"errors"
	"fmt"
	"io"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// NALReader提供无封装的完整NAL。Payload至少在下一次ReadNAL前有效；
// AUReader会复制需要保留的数据。输入的单NAL预算由具体Reader负责。
type NALReader interface {
	ReadNAL() (nal.Unit, error)
}

// BoundaryMode选择AU边界策略；两种策略均解析图像标识并遵守AUD/前后缀规则。
type BoundaryMode uint8

const (
	// BoundaryPicture严格比较图像标识；不把first_mb=0单独视为新图像。
	BoundaryPicture BoundaryMode = iota
	// BoundaryFirstMB额外将非冗余first_mb=0视为新图像，适合已约定顺序的流。
	// 它不能用于依赖乱序slice或重复首slice的输入。
	BoundaryFirstMB
)

// AccessUnitConfig限制正在组装的AU；零值分别使用32MiB与4096个NAL。
// 字节预算包含每NAL四字节Annex-B起始码及header，不是pipeline的MaxInputBuffer。
type AccessUnitConfig struct {
	MaxAccessUnitSize int
	MaxNALUnits       int
	BoundaryMode      BoundaryMode
}

// AUReader将AnnexBReader或AVCCReader等NAL源组装成完整Annex-B Packet。
// 参数集按输入顺序生效；默认按Spec: 7.4.1.2.3/4比较图像标识。
// Packet.Data独立归调用方，KeyFrame仅标识IDR；裸流无时钟，PTS/DTS/Duration均为0。
// 只验证参数集和slice header，不证明slice data完整或像素可解码。
// 实例非并发安全，不关闭源，不创建goroutine，不中断底层阻塞读取。
type AUReader struct {
	src            NALReader
	cfg            AccessUnitConfig
	assembler      *nal.Assembler
	size           int
	hasVCL, closed bool
	lastUnit       nal.Unit
	lastInfo       *nal.SliceInfo
	sps            map[uint32]*syntax.SPS
	pps            map[uint32]*syntax.PPS
	partitions     map[uint32]*partitionEntry
	err            error
}

var _ AccessUnitReader = (*AUReader)(nil)

// NewAccessUnitReader创建有界AU适配器；不提前消费输入。
func NewAccessUnitReader(src NALReader, cfg AccessUnitConfig) (*AUReader, error) {
	if src == nil || cfg.MaxAccessUnitSize < 0 || cfg.MaxNALUnits < 0 || cfg.BoundaryMode > BoundaryFirstMB {
		return nil, fmt.Errorf("stream: AU配置无效: %w", h264.ErrInvalidData)
	}
	if cfg.MaxAccessUnitSize == 0 {
		cfg.MaxAccessUnitSize = 32 << 20
	}
	if cfg.MaxNALUnits == 0 {
		cfg.MaxNALUnits = 4096
	}
	return &AUReader{src: src, cfg: cfg, assembler: nal.NewAssembler(),
		sps: map[uint32]*syntax.SPS{}, pps: map[uint32]*syntax.PPS{}}, nil
}

// ReadAccessUnit在看到下一AU首NAL或正常EOF后交付当前AU；最多预读一个NAL。
// EOF前最后一个含VCL的AU仍会交付；仅参数集/AUD的尾部返回ErrInvalidData，
// 传输/语法/资源错误均粘性，不将未完成AU伪报为合法输出。
func (r *AUReader) ReadAccessUnit() (h264.Packet, error) {
	if r.err != nil {
		return h264.Packet{}, r.err
	}
	for {
		u, err := r.src.ReadNAL()
		if errors.Is(err, io.EOF) {
			if r.assembler.Pending() == 0 {
				return r.fail(io.EOF)
			}
			if !r.hasVCL {
				return r.fail(fmt.Errorf("stream: 尾部AU缺少VCL: %w", h264.ErrInvalidData))
			}
			p := r.take()
			r.err = io.EOF
			return p, nil
		}
		if err != nil {
			return r.fail(fmt.Errorf("stream: 读取AU: %w", err))
		}
		// 先做减法预算检查，避免len+header+prefix溢出或转换RBSP后才拒绝。
		if r.cfg.MaxAccessUnitSize < 5 || len(u.Payload) > r.cfg.MaxAccessUnitSize-5 {
			return r.fail(fmt.Errorf("stream: 单NAL超出AU预算: %w", h264.ErrResourceLimit))
		}
		info, err := r.inspect(u)
		if err != nil {
			return r.fail(fmt.Errorf("stream: AU %s: %w", u.Header.Type, err))
		}
		boundary := false
		if r.hasVCL {
			if u.IsVCL() {
				boundary = r.closed || nal.DetectPictureBoundary(r.lastInfo, info) == nal.BoundaryNew ||
					(r.cfg.BoundaryMode == BoundaryFirstMB && u.Header.Type != nal.TypeSlicePartitionB && u.Header.Type != nal.TypeSlicePartitionC && info.FirstMBInSlice == 0 && info.RedundantPicCnt == 0)
			} else {
				boundary = nal.DetectBoundary(r.lastUnit, u, nil, nil) == nal.BoundaryNew ||
					(r.closed && u.Header.Type != nal.TypeEndOfSequence && u.Header.Type != nal.TypeEndOfStream)
			}
		}
		var completed h264.Packet
		if boundary {
			completed = r.take()
		}
		n := len(u.Payload) + 5
		if n > r.cfg.MaxAccessUnitSize-r.size || r.assembler.Pending() >= r.cfg.MaxNALUnits {
			return r.fail(fmt.Errorf("stream: AU字节或NAL数量超限: %w", h264.ErrResourceLimit))
		}
		// 支持复用底层NAL缓冲的源；后续ReadNAL不影响挂起AU。
		u.Payload = append([]byte(nil), u.Payload...)
		r.assembler.PushHinted(u, info, nal.BoundarySame)
		r.size += n
		if u.IsVCL() {
			r.hasVCL, r.lastUnit, r.lastInfo = true, nal.Unit{Header: u.Header}, info
		}
		if u.Header.Type == nal.TypeEndOfSequence || u.Header.Type == nal.TypeEndOfStream {
			r.closed = true
		}
		if completed.Valid() {
			return completed, nil
		}
	}
}

func (r *AUReader) take() h264.Packet {
	au := r.assembler.Flush()
	p := h264.Packet{Data: make([]byte, 0, r.size), KeyFrame: au.IsIDR()}
	for _, u := range au.Units {
		p.Data = append(p.Data, 0, 0, 0, 1, u.Header.Byte())
		p.Data = append(p.Data, u.Payload...)
	}
	// 丢弃Assembler容量中的旧NAL引用，避免后续小AU保留先前大载荷。
	r.assembler = nal.NewAssembler()
	r.size, r.hasVCL, r.closed, r.lastInfo = 0, false, false, nil
	r.lastUnit = nal.Unit{}
	return p
}

func (r *AUReader) fail(err error) (h264.Packet, error) {
	r.err = err
	r.assembler = nal.NewAssembler()
	r.lastInfo, r.sps, r.pps = nil, nil, nil
	r.lastUnit = nal.Unit{}
	return h264.Packet{}, err
}
