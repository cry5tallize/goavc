package picture

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
)

// validateLayout 在任何尺寸乘法和分配前校验构造与池共用的布局契约。
// 扩展格式只有存储与访问路径完成后才能进入允许集合。
func validateLayout(mbWidth, mbHeight int, format h264.PixelFormat) error {
	if mbWidth <= 0 || mbHeight <= 0 {
		return fmt.Errorf("picture: invalid macroblock dimensions %dx%d: %w", mbWidth, mbHeight, h264.ErrInvalidData)
	}
	if mbWidth > h264.MaxFrameWidth/16 || mbHeight > h264.MaxFrameHeight/16 {
		return fmt.Errorf("picture: dimensions %dx%d MB exceed limit: %w", mbWidth, mbHeight, h264.ErrResourceLimit)
	}
	if format != h264.YUV420P && format != h264.YUV420P10LE && format != h264.YUV422P && format != h264.YUV422P10LE && format != h264.YUV444P && format != h264.YUV444P10LE {
		return fmt.Errorf("picture: pixel format %s: %w", format, h264.ErrUnsupported)
	}
	return nil
}
