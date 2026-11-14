package stream

import (
	"errors"
	"fmt"
	"io"
	"math"

	h264 "github.com/goavc/h264"
)

type planeLayout struct{ rowBytes, height, bytes int }

// rawLayout先限界再乘法；8192²×3×2在32位int内，行宽以字节计。
func rawLayout(width, height int, format h264.PixelFormat) ([3]planeLayout, int, error) {
	var planes [3]planeLayout
	if width <= 0 || height <= 0 {
		return planes, 0, fmt.Errorf("stream: 无效尺寸%d×%d: %w", width, height, h264.ErrInvalidData)
	}
	if width > h264.MaxFrameWidth || height > h264.MaxFrameHeight {
		return planes, 0, fmt.Errorf("stream: YUV尺寸超限: %w", h264.ErrResourceLimit)
	}
	if !format.Valid() {
		return planes, 0, fmt.Errorf("stream: 未知YUV格式: %w", h264.ErrUnsupported)
	}
	total := 0
	for comp := range planes {
		w, h := format.PlaneDimensions(comp, width, height)
		rowBytes := w * format.BytesPerSample()
		planes[comp] = planeLayout{rowBytes, h, rowBytes * h}
		total += rowBytes * h
	}
	return planes, total, nil
}

// YUVReader读取紧密Y/U/V平面。每帧独立分配，调用方负责Release。
// PTS从0逐帧递增、Duration=1，单位为一帧，不推断帧率；KeyFrame为false。
// 不关闭底层Reader；实例非并发安全，任何I/O失败后重复返回相同错误。
type YUVReader struct {
	src           io.Reader
	width, height int
	format        h264.PixelFormat
	planes        [3]planeLayout
	bytes         int
	nextPTS       int64
	err           error
}

var _ FrameReader = (*YUVReader)(nil)

// NewYUVReader限制正尺寸至8192×8192；支持奇数显示尺寸，构造时不读数据或分配帧。
func NewYUVReader(src io.Reader, width, height int, format h264.PixelFormat) (*YUVReader, error) {
	if src == nil {
		return nil, fmt.Errorf("stream: nil YUV reader: %w", h264.ErrInvalidData)
	}
	planes, size, err := rawLayout(width, height, format)
	if err != nil {
		return nil, err
	}
	return &YUVReader{src: src, width: width, height: height, format: format, planes: planes, bytes: size}, nil
}

// ReadFrame仅在帧边界返回io.EOF。尾帧截断同时匹配ErrInvalidData和io.ErrUnexpectedEOF，
// 不返回部分帧；底层其他I/O错误原样保留errors.Is语义。
func (r *YUVReader) ReadFrame() (*h264.Frame, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.nextPTS == math.MaxInt64 {
		r.err = fmt.Errorf("stream: YUV时间戳超限: %w", h264.ErrResourceLimit)
		return nil, r.err
	}
	// 先探测一个字节，避免空流或重复EOF为大尺寸配置分配整帧。
	var first [1]byte
	if _, err := io.ReadFull(r.src, first[:]); err != nil {
		r.err = err
		return nil, err
	}
	data := make([]byte, r.bytes)
	data[0] = first[0]
	if _, err := io.ReadFull(r.src, data[1:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			err = fmt.Errorf("stream: YUV尾帧截断: %w: %w", h264.ErrInvalidData, io.ErrUnexpectedEOF)
		} else {
			err = fmt.Errorf("stream: 读取YUV帧: %w", err)
		}
		r.err = err
		return nil, err
	}
	f := &h264.Frame{Width: r.width, Height: r.height, Format: r.format, PTS: r.nextPTS, Duration: 1}
	start := 0
	for comp, pl := range r.planes {
		end := start + pl.bytes
		f.Planes[comp] = h264.Plane{Data: data[start:end:end], Stride: pl.rowBytes}
		start = end
	}
	if r.format.BitDepth() == 10 {
		if err := f.Validate(); err != nil {
			f.Release()
			r.err = fmt.Errorf("stream: 非法高位深YUV采样: %w", err)
			return nil, r.err
		}
	}
	r.nextPTS++
	return f, nil
}
