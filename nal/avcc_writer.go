package nal

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// AVCCWriter 写出 AVCC（length-prefixed）格式的 NAL unit。
//
// 长度前缀固定为 1..4 字节大端，长度只包含 NAL 本身（header + payload）。
// 长度在写入前会检查是否能放入所选宽度；检查失败时不写出任何字节，
// 避免在输出流中留下半个 NAL。
type AVCCWriter struct {
	w          io.Writer
	lengthSize int
	prefix     [4]byte
}

// NewAVCCWriter 创建 AVCC 写出器。lengthSize 必须是 1..4。
func NewAVCCWriter(w io.Writer, lengthSize int) (*AVCCWriter, error) {
	if lengthSize < 1 || lengthSize > 4 {
		return nil, fmt.Errorf("nal: avcc length size %d must be 1..4", lengthSize)
	}
	return &AVCCWriter{w: w, lengthSize: lengthSize}, nil
}

// Write 写出一个 NAL unit。
func (w *AVCCWriter) Write(u Unit) error {
	raw := u.Bytes()
	length := uint64(len(raw))
	maxLength := uint64(1)<<(8*w.lengthSize) - 1
	if length > maxLength {
		return fmt.Errorf("nal: nal length %d does not fit in %d-byte prefix", length, w.lengthSize)
	}

	switch w.lengthSize {
	case 1:
		w.prefix[0] = byte(length)
	case 2:
		binary.BigEndian.PutUint16(w.prefix[:2], uint16(length))
	case 3:
		w.prefix[0] = byte(length >> 16)
		w.prefix[1] = byte(length >> 8)
		w.prefix[2] = byte(length)
	default:
		binary.BigEndian.PutUint32(w.prefix[:4], uint32(length))
	}

	if err := writeAll(w.w, w.prefix[:w.lengthSize]); err != nil {
		return fmt.Errorf("nal: write avcc length prefix: %w", err)
	}
	if err := writeAll(w.w, raw); err != nil {
		return fmt.Errorf("nal: write avcc nal: %w", err)
	}
	return nil
}

// MarshalAVCC 把若干 NAL unit 序列化为内存中的 AVCC 数据。
func MarshalAVCC(units []Unit, lengthSize int) ([]byte, error) {
	var buf bytes.Buffer
	w, err := NewAVCCWriter(&buf, lengthSize)
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		if err := w.Write(u); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// writeAll 处理短写；io.Writer 不应在 n < len 时返回 nil，但这里仍做防御。
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
