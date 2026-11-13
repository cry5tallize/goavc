package nal

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	h264 "github.com/goavc/h264"
)

// DefaultMaxNALSize 是 AVCC 读取器的默认单 NAL 上限（8 MiB）。
//
// 长度前缀来自不可信输入；如果没有上限，一个 0xFFFFFFFF 前缀会让解析器
// 直接尝试分配 4 GiB。调用方可以修改 AVCCReader.MaxNALSize。
const DefaultMaxNALSize = 8 << 20

// AVCCReader 读取 AVCC（length-prefixed）格式的 NAL unit 流。
//
// 每个 NAL 前有一个 1..4 字节的大端长度前缀，长度只包含 NAL 本身
// （header + payload），不包含前缀。长度前缀的字节数由 avcC 的
// lengthSizeMinusOne 决定，本读取器不做自动猜测。
//
// 与 Annex-B 扫描器一样，Next 返回的 payload 是独立分配的；
// 错误是粘性的（io.EOF 除外）。
type AVCCReader struct {
	r          io.Reader
	lengthSize int
	lenBuf     [4]byte

	// MaxNALSize 是允许的最大 NAL 字节数；<= 0 表示不限制。
	// 默认值为 DefaultMaxNALSize。
	MaxNALSize int

	err       error
	unitIndex int
}

// NewAVCCReader 创建 AVCC 读取器。lengthSize 必须是 1..4。
func NewAVCCReader(r io.Reader, lengthSize int) (*AVCCReader, error) {
	if lengthSize < 1 || lengthSize > 4 {
		return nil, fmt.Errorf("nal: avcc length size %d must be 1..4", lengthSize)
	}
	return &AVCCReader{
		r:          r,
		lengthSize: lengthSize,
		MaxNALSize: DefaultMaxNALSize,
	}, nil
}

// Next 读取下一个 NAL unit；流正常结束时返回 io.EOF。
func (r *AVCCReader) Next() (Unit, error) {
	if r.err != nil {
		return Unit{}, r.err
	}

	if _, err := io.ReadFull(r.r, r.lenBuf[:r.lengthSize]); err == io.EOF {
		return Unit{}, io.EOF
	} else if err != nil {
		return Unit{}, r.fail(fmt.Errorf("nal: avcc unit %d length prefix: %w", r.unitIndex, r.wrapReadErr(err)))
	}

	var length uint32
	switch r.lengthSize {
	case 1:
		length = uint32(r.lenBuf[0])
	case 2:
		length = uint32(binary.BigEndian.Uint16(r.lenBuf[:2]))
	case 3:
		length = uint32(r.lenBuf[0])<<16 | uint32(r.lenBuf[1])<<8 | uint32(r.lenBuf[2])
	default:
		length = binary.BigEndian.Uint32(r.lenBuf[:4])
	}
	if length == 0 {
		return Unit{}, r.fail(fmt.Errorf("nal: avcc unit %d has zero length: %w", r.unitIndex, h264.ErrInvalidData))
	}
	if r.MaxNALSize > 0 && uint64(length) > uint64(r.MaxNALSize) {
		return Unit{}, r.fail(fmt.Errorf("nal: avcc unit %d length %d exceeds limit %d: %w",
			r.unitIndex, length, r.MaxNALSize, h264.ErrResourceLimit))
	}
	maxInt := uint64(^uint(0) >> 1)
	if uint64(length) > maxInt {
		return Unit{}, r.fail(fmt.Errorf("nal: avcc unit %d length %d exceeds int range: %w",
			r.unitIndex, length, h264.ErrResourceLimit))
	}

	buf := make([]byte, int(length))
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return Unit{}, r.fail(fmt.Errorf("nal: avcc unit %d payload truncated: %w", r.unitIndex, r.wrapReadErr(err)))
	}

	h, err := ParseHeader(buf[0])
	if err != nil {
		return Unit{}, r.fail(fmt.Errorf("nal: avcc unit %d header: %w", r.unitIndex, err))
	}
	r.unitIndex++
	return Unit{Header: h, Payload: buf[1:]}, nil
}

// fail 记录粘性错误并返回它。
func (r *AVCCReader) fail(err error) error {
	r.err = err
	return err
}

// wrapReadErr 把 payload/前缀的截断统一映射为 ErrInvalidData。
func (r *AVCCReader) wrapReadErr(err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return fmt.Errorf("unexpected end of stream: %w", h264.ErrInvalidData)
	}
	return err
}

// SplitAVCC 一次性解析内存中的 AVCC 数据。
func SplitAVCC(data []byte, lengthSize int) ([]Unit, error) {
	reader, err := NewAVCCReader(bytes.NewReader(data), lengthSize)
	if err != nil {
		return nil, err
	}
	var units []Unit
	for {
		u, err := reader.Next()
		if err == io.EOF {
			return units, nil
		}
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
}
