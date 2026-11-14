package stream

import (
	"errors"
	"fmt"
	"io"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/nal"
)

// AnnexBReader按NAL读取Annex-B字节流；返回EBSP，不组装AU或推断时间戳。
// 复用nal扫描器的跨块缓冲和大小门禁；每次返回独立Payload，调用方持有。
// 仅验证字节封装与NAL header，RBSP/slice完整性由后续语法/解码层验证。
// 实例非并发安全，不关闭底层Reader，也不能中断其阻塞Read。
type AnnexBReader struct {
	input   annexBInput
	scanner *nal.AnnexBScanner
	err     error
}

// NewAnnexBReader设置单NAL上限（含header，不含起始码/尾零）；0使用8MiB，
// 负值拒绝，不提供隐式无限缓存。内部另有扫描器固定64KiB读缓冲。
func NewAnnexBReader(src io.Reader, maxNALSize int) (*AnnexBReader, error) {
	if src == nil || maxNALSize < 0 {
		return nil, fmt.Errorf("stream: Annex-B输入或NAL上限无效: %w", h264.ErrInvalidData)
	}
	if maxNALSize == 0 {
		maxNALSize = nal.DefaultMaxNALSize
	}
	r := &AnnexBReader{input: annexBInput{src: src}}
	r.scanner = nal.NewAnnexBScannerWithLimit(&r.input, maxNALSize)
	return r, nil
}

// ReadNAL在真实EOF前等待下一个边界；最后一个NAL在EOF时返回。
// 空流正常EOF；纯前导零缺少完整起始码或末尾起始码缺少NAL同时匹配ErrInvalidData/
// io.ErrUnexpectedEOF。所有错误与EOF均粘性，不重读、不尝试重同步。
func (r *AnnexBReader) ReadNAL() (nal.Unit, error) {
	if r.err != nil {
		return nal.Unit{}, r.err
	}
	u, err := r.scanner.Next()
	if err == nil {
		return u, nil
	}
	if errors.Is(err, io.EOF) {
		if r.input.hasData && (!r.input.started || r.input.emptyNAL) {
			err = fmt.Errorf("stream: Annex-B尾部缺少起始码或NAL: %w: %w", h264.ErrInvalidData, io.ErrUnexpectedEOF)
		}
	} else {
		err = fmt.Errorf("stream: 读取Annex-B NAL: %w", err)
	}
	r.err = err
	return nal.Unit{}, err
}

// annexBInput只观察前导/终端封装，不复制输入或拆分NAL。
// nal.Scanner允许跳过任意前导垃圾并忽略空尾起始码；流适配器明确拒绝这两种
// 情况。零游程饱和到2，状态大小与输入长度无关；尾部检查仅在扫描器EOF时使用，
// 不能因底层预读到坏尾部而丢掉尚可返回的前一个NAL。Spec: Annex B。
type annexBInput struct {
	src                        io.Reader
	zeros                      uint8
	hasData, started, emptyNAL bool
}

func (r *annexBInput) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	// 扫描器以io.EOF结束最后一个NAL，适配层统一包装EOF的流控语义。
	if errors.Is(err, io.EOF) {
		err = io.EOF
	}
	for _, b := range p[:n] {
		r.hasData = true
		if b == 0 {
			if r.zeros < 2 {
				r.zeros++
			}
			continue
		}
		prefix := r.zeros == 2 && b == 1
		if !r.started && !prefix {
			return 0, fmt.Errorf("stream: Annex-B前导非零垃圾: %w", h264.ErrInvalidData)
		}
		if prefix {
			r.started, r.emptyNAL = true, true
		} else {
			r.emptyNAL = false
		}
		r.zeros = 0
	}
	return n, err
}
