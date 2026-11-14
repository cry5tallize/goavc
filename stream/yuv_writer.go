package stream

import (
	"fmt"
	"io"

	h264 "github.com/goavc/h264"
)

// YUVWriter写紧密Y/U/V，只消费各平面的活动行，忽略stride填充。
// 原始YUV不携带尺寸、格式或时间戳；调用方须保持整条流的布局约定一致。
// 不关闭底层Writer，不保留输入引用，实例非并发安全。
type YUVWriter struct {
	dst io.Writer
	err error
}

var _ FrameWriter = (*YUVWriter)(nil)

// NewYUVWriter创建同步原始帧输出适配器；底层Writer的关闭权仍属于调用方。
func NewYUVWriter(dst io.Writer) (*YUVWriter, error) {
	if dst == nil {
		return nil, fmt.Errorf("stream: nil YUV writer: %w", h264.ErrInvalidData)
	}
	return &YUVWriter{dst: dst}, nil
}

// WriteFrame在任何写出前校验全部平面，结构错误可更正后重试。
// 传输错误或短写后位置已不可靠，后续调用重复返回该错误，不继续追加帧。
func (w *YUVWriter) WriteFrame(f *h264.Frame) error {
	if w.err != nil {
		return w.err
	}
	if f == nil {
		return fmt.Errorf("stream: nil YUV frame: %w", h264.ErrInvalidData)
	}
	layout, _, err := rawLayout(f.Width, f.Height, f.Format)
	if err != nil {
		return err
	}
	if err := f.Validate(); err != nil {
		return err
	}
	for comp, pl := range layout {
		p := f.Planes[comp]
		for y := 0; y < pl.height; y++ {
			row := p.Data[y*p.Stride : y*p.Stride+pl.width]
			n, err := w.dst.Write(row)
			if err == nil && n != len(row) {
				err = io.ErrShortWrite
			}
			if err != nil {
				w.err = fmt.Errorf("stream: 写YUV平面%d行%d: %w", comp, y, err)
				return w.err
			}
		}
	}
	return nil
}
