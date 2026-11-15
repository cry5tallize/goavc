package stream

import (
	"errors"
	"fmt"
	"io"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

// AVCCReader按长度前缀读取独立EBSP NAL，不组装AU或生成时间戳。
// 实例非并发安全；不关闭底层Reader，也不提供阻塞Read取消。
type AVCCReader struct {
	reader  *nal.AVCCReader
	pending []nal.Unit
	err     error
}

// NewAVCCReader配置1..4字节大端前缀；maxNALSize含header，0使用8MiB。
// 不猜测封装或前缀宽度，负上限拒绝；复用NAL层的分配前大小校验。
func NewAVCCReader(src io.Reader, lengthSize, maxNALSize int) (*AVCCReader, error) {
	if src == nil || lengthSize < 1 || lengthSize > 4 || maxNALSize < 0 {
		return nil, fmt.Errorf("stream: AVCC输入、前缀宽度或上限无效: %w", h264.ErrInvalidData)
	}
	if maxNALSize == 0 {
		maxNALSize = nal.DefaultMaxNALSize
	}
	reader, err := nal.NewAVCCReader(avccInput{src}, lengthSize)
	if err != nil {
		return nil, err
	}
	reader.MaxNALSize = maxNALSize
	return &AVCCReader{reader: reader}, nil
}

// NewAVCCReaderFromConfig从调用方提供的avcC记录取得前缀宽度，不解析MP4 box。
// ReadNAL先交付记录中的SPS/PPS/SPS扩展，再读样本；构造后不持有原始记录引用。
// 参数集在复制前按同一NAL上限检查；全部验证完成之前不消费样本输入。
func NewAVCCReaderFromConfig(src io.Reader, avcC []byte, maxNALSize int) (*AVCCReader, error) {
	// 标准字段最多31个SPS、255个PPS、255个SPS扩展，单项16-bit长度。
	// 先限制记录总长度，避免对带巨大垃圾尾部的记录先复制全部参数集。
	const maxConfigSize = 11 + (31+255+255)*(2+65535)
	if len(avcC) > maxConfigSize {
		return nil, fmt.Errorf("stream: avcC记录超限: %w", h264.ErrResourceLimit)
	}
	r, err := NewAVCCReader(src, 4, maxNALSize)
	if err != nil {
		return nil, err
	}
	limit := r.reader.MaxNALSize
	cfg, err := nal.ParseAVCDecoderConfigWithLimit(avcC, limit)
	if err != nil {
		return nil, fmt.Errorf("stream: 读取avcC配置: %w", err)
	}
	for _, group := range []struct {
		typ  nal.UnitType
		data [][]byte
	}{{nal.TypeSPS, cfg.SPS}, {nal.TypePPS, cfg.PPS}, {nal.TypeSPSExtension, cfg.SPSExt}} {
		for _, raw := range group.data {
			h, err := nal.ParseHeader(raw[0])
			if err != nil || h.Type != group.typ {
				return nil, fmt.Errorf("stream: avcC参数集类型不符: %w", h264.ErrInvalidData)
			}
			r.pending = append(r.pending, nal.Unit{Header: h, Payload: raw[1:]})
		}
	}
	r.reader, err = nal.NewAVCCReader(avccInput{src}, cfg.LengthSize())
	if err != nil {
		return nil, err
	}
	r.reader.MaxNALSize = limit
	return r, nil
}

// ReadNAL返回独立Payload；帧边界没有语义，只有NAL边界EOF。
// 零长度、坏header、截断前缀/载荷返回ErrInvalidData，超限返回ErrResourceLimit。
// 错误和EOF均粘性；不重同步，也不将NAL当作可独立解码的完整帧。
func (r *AVCCReader) ReadNAL() (nal.Unit, error) {
	if r.err != nil {
		return nal.Unit{}, r.err
	}
	if len(r.pending) != 0 {
		u := r.pending[0]
		r.pending[0] = nal.Unit{}
		r.pending = r.pending[1:]
		return u, nil
	}
	u, err := r.reader.Next()
	if err == nil {
		return u, nil
	}
	if errors.Is(err, io.EOF) {
		r.err = io.EOF
	} else {
		r.err = fmt.Errorf("stream: 读取AVCC NAL: %w", err)
	}
	return nal.Unit{}, r.err
}

type avccInput struct{ src io.Reader }

func (r avccInput) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if errors.Is(err, io.EOF) {
		err = io.EOF
	} else if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
