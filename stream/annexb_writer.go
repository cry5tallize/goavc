package stream

import (
	"fmt"
	"io"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/nal"
)

// AnnexBWriterConfig分别限制单NAL（含header）、输入/规范化输出AU字节数及NAL数。
// 零值分别使用8MiB、32MiB、4096；负值无效。AU预算包含起始码和尾零。
type AnnexBWriterConfig struct {
	MaxNALSize        int
	MaxAccessUnitSize int
	MaxNALUnits       int
}

// AnnexBWriter同步消费Annex-B Packet，复用NAL Writer统一写四字节起始码。
// 规范化时丢弃Annex B前导/尾零，NAL的EBSP原样保留。
// 调用方须提供单个完整AU；这里只验证封装、header和资源预算，不解析slice或验证AU分组。
// 不猜测AVCC，不序列化时间戳/KeyFrame，不保留Packet.Data，不关闭底层Writer。
// 实例非并发安全；无内部goroutine、缓存flush或阻塞I/O取消能力。
type AnnexBWriter struct {
	writer *nal.AnnexBWriter
	cfg    AnnexBWriterConfig
	err    error
}

var _ AccessUnitWriter = (*AnnexBWriter)(nil)

// NewAnnexBWriter创建有界同步输出适配器，不提前写出数据。
func NewAnnexBWriter(dst io.Writer, cfg AnnexBWriterConfig) (*AnnexBWriter, error) {
	if dst == nil || cfg.MaxNALSize < 0 || cfg.MaxAccessUnitSize < 0 || cfg.MaxNALUnits < 0 {
		return nil, fmt.Errorf("stream: Annex-B Writer配置无效: %w", h264.ErrInvalidData)
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
	return &AnnexBWriter{writer: nal.NewAnnexBWriter(accessUnitOutput{dst}), cfg: cfg}, nil
}

// WriteAccessUnit在任何写出之前验证整个封装；结构/资源错误不污染Writer，可更正后重试。
// 空Packet不是flush；缺少VCL或不完整起始码返回ErrInvalidData。
// 短写或底层I/O错误发生后，后续调用返回同一错误，不继续写半个AU。
func (w *AnnexBWriter) WriteAccessUnit(p h264.Packet) error {
	if w.err != nil {
		return w.err
	}
	units, err := collectAnnexBAccessUnit(p.Data, w.cfg.MaxNALSize, w.cfg.MaxAccessUnitSize, w.cfg.MaxNALUnits, 4)
	if err != nil {
		return err
	}
	for _, u := range units {
		if err := w.writer.Write(u); err != nil {
			w.err = fmt.Errorf("stream: 写Annex-B AU: %w", err)
			return w.err
		}
	}
	return nil
}
