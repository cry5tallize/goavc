package stream

import (
	"fmt"
	"io"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

// AVCCWriterConfig配置输出大端长度前缀及资源上限。
// LengthSize为1..4，0默认4；MaxNALSize含header，0默认8MiB，且受前缀容量限制。
// MaxAccessUnitSize同时限制Annex-B输入与AVCC输出，0默认32MiB；MaxNALUnits为0默认4096。
// 所有负值均无效。长度前缀只计NAL header和EBSP，不包含前缀本身。
type AVCCWriterConfig struct {
	LengthSize        int
	MaxNALSize        int
	MaxAccessUnitSize int
	MaxNALUnits       int
}

// AVCCWriter将调用方已分组的Annex-B Packet同步重封装为AVCC，复用NAL Writer。
// 与AUReader/Encoder输出直接衔接；输入不猜测AVCC，输出不含avcC记录或MP4 box。
// 完整封装/header/长度容量与预算验证后才写出，不验证RBSP/slice或AU分组。
// 时间戳/KeyFrame不序列化，不保留Packet.Data，不关闭底层Writer；实例非并发安全。
// 不创建goroutine，也不提供flush缓存或底层阻塞写取消。
type AVCCWriter struct {
	writer *nal.AVCCWriter
	cfg    AVCCWriterConfig
	err    error
}

var _ AccessUnitWriter = (*AVCCWriter)(nil)

// NewAVCCWriter创建有界重封装器；构造不写出任何字节。
func NewAVCCWriter(dst io.Writer, cfg AVCCWriterConfig) (*AVCCWriter, error) {
	if dst == nil || cfg.LengthSize < 0 || cfg.LengthSize > 4 || cfg.MaxNALSize < 0 || cfg.MaxAccessUnitSize < 0 || cfg.MaxNALUnits < 0 {
		return nil, fmt.Errorf("stream: AVCC Writer配置无效: %w", h264.ErrInvalidData)
	}
	if cfg.LengthSize == 0 {
		cfg.LengthSize = 4
	}
	if cfg.MaxNALSize == 0 {
		cfg.MaxNALSize = nal.DefaultMaxNALSize
	}
	if cfg.MaxAccessUnitSize == 0 {
		cfg.MaxAccessUnitSize = 32 << 20
	}
	if cfg.MaxNALUnits == 0 {
		cfg.MaxNALUnits = 4096
	}
	// 在扫描分配前收紧单NAL预算；uint64避免4字节前缀在32位平台溢出。
	maxLength := uint64(1)<<(8*cfg.LengthSize) - 1
	if uint64(cfg.MaxNALSize) > maxLength {
		cfg.MaxNALSize = int(maxLength)
	}
	writer, err := nal.NewAVCCWriter(accessUnitOutput{dst}, cfg.LengthSize)
	if err != nil {
		return nil, err
	}
	return &AVCCWriter{writer: writer, cfg: cfg}, nil
}

// WriteAccessUnit先验证全部输入，坏尾部或后续NAL装不进前缀时也不提前写出。
// 结构/资源错误可更正重试；空Packet不是flush。短写或I/O失败后错误粘性，
// 后续调用不会继续消费或追加数据，调用方须放弃已经部分写出的流。
func (w *AVCCWriter) WriteAccessUnit(p h264.Packet) error {
	if w.err != nil {
		return w.err
	}
	units, err := collectAnnexBAccessUnit(p.Data, w.cfg.MaxNALSize, w.cfg.MaxAccessUnitSize, w.cfg.MaxNALUnits, w.cfg.LengthSize)
	if err != nil {
		return err
	}
	for _, u := range units {
		if err := w.writer.Write(u); err != nil {
			w.err = fmt.Errorf("stream: 写AVCC AU: %w", err)
			return w.err
		}
	}
	return nil
}
