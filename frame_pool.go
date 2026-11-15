package h264

import (
	"fmt"
	"sync"

	"github.com/goavc/h264/internal/buffer"
)

const (
	frameBackingMaxBytes uint64 = 64 << 20
	frameBackingPerKey          = 4
	frameBackingMaxKeys         = 8
)

var frameBackingPool = buffer.NewPool(frameBackingMaxBytes, frameBackingPerKey, frameBackingMaxKeys)

type frameLease struct {
	once sync.Once
	key  uint64
	slot buffer.Slot
}

func (l *frameLease) release() {
	l.once.Do(func() {
		frameBackingPool.Put(l.key, l.slot)
	})
}

// NewFrame 创建一张拥有独立生命周期的紧凑公共帧。
//
// width/height 为显示尺寸，允许奇数但必须为正且不超过公共 8192 上限。
// 10-bit 格式仍使用公共 Frame 的显式 LE byte 存储。返回帧的有效像素区域
// 已清零；仅 backing storage 可复用，Frame 头不会复用。调用方用完必须 Release。
func NewFrame(width, height int, format PixelFormat) (*Frame, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("frame: invalid dimensions %dx%d: %w", width, height, ErrInvalidData)
	}
	if width > MaxFrameWidth || height > MaxFrameHeight {
		return nil, fmt.Errorf("frame: dimensions %dx%d exceed limit: %w", width, height, ErrResourceLimit)
	}
	if !format.Valid() {
		return nil, fmt.Errorf("frame: pixel format %d is unsupported: %w", format, ErrUnsupported)
	}

	var sizes [3]int
	for i := range sizes {
		_, rows := format.PlaneDimensions(i, width, height)
		rowBytes := format.RowBytes(i, width)
		if rows <= 0 || rowBytes <= 0 || rows > maxInt()/rowBytes {
			return nil, fmt.Errorf("frame: plane %d size overflows: %w", i, ErrResourceLimit)
		}
		sizes[i] = rowBytes * rows
	}

	key := frameBackingKey(width, height, format)
	slot, ok := frameBackingPool.Get(key, sizes)
	if !ok {
		for i, size := range sizes {
			slot.Planes[i] = make([]byte, size)
		}
	}
	for i := range slot.Planes {
		clear(slot.Planes[i])
	}

	lease := &frameLease{key: key, slot: slot}
	frame := &Frame{Width: width, Height: height, Format: format, onRelease: lease.release}
	for i, size := range sizes {
		frame.Planes[i] = Plane{Data: slot.Planes[i][:size], Stride: format.RowBytes(i, width)}
	}
	return frame, nil
}

func frameBackingKey(width, height int, format PixelFormat) uint64 {
	return uint64(width)<<16 | uint64(height)<<3 | uint64(format)
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
