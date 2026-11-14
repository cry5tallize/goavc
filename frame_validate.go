package h264

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Frame.Validate 检查 Frame 的结构自洽性：格式合法、尺寸为正、每个平面的
// stride与缓冲长度足够；10-bit有效采样须在0..1023内（不检查行尾填充）。
//
// 为什么必须做溢出检查：Width/Height 可能来自不可信码流（SPS 字段）或
// 调用方误填。若直接算 stride*rows，在 32 位平台上会回绕成一个很小的正数，
// 随后按小缓冲做大循环就会越界读写。这里所有乘法都走 mulOK/addOK。
//
// 检查顺序刻意从"便宜且能提前拒绝"的条件开始：
// 尺寸 → 格式 → 逐平面几何 → stride → 容量 → 高位深采样范围。
//
// 注意：本方法不施加资源上限（分辨率、总内存）。
// 上限属于策略，由 DecoderConfig / EncoderConfig 负责，两者分离是为了让
// 同一个 Frame 类型既能用于小分辨率低延迟场景，也能用于大分辨率离线场景。
func (f *Frame) Validate() error {
	if f == nil {
		return fmt.Errorf("%w: nil frame", ErrInvalidData)
	}
	if f.Width <= 0 || f.Height <= 0 {
		return fmt.Errorf("%w: invalid dimensions %dx%d", ErrInvalidData, f.Width, f.Height)
	}
	if !f.Format.Valid() {
		return fmt.Errorf("%w: pixel format %d", ErrUnsupported, uint8(f.Format))
	}

	bps := f.Format.BytesPerSample()
	for i := 0; i < 3; i++ {
		w, h := f.Format.PlaneDimensions(i, f.Width, f.Height)
		rowBytes, ok := mulOK(w, bps)
		if !ok {
			return fmt.Errorf("%w: plane %d row width %d overflows", ErrInvalidData, i, w)
		}
		p := f.Planes[i]

		// 空缓冲单独报错，否则后面的容量比较会退化成 "stride 太小"，
		// 掩盖真正的原因（调用方忘记填充 plane）。
		if len(p.Data) == 0 {
			return fmt.Errorf("%w: plane %d has no data", ErrInvalidData, i)
		}
		if p.Stride < rowBytes {
			return fmt.Errorf("%w: plane %d stride %d below row size %d", ErrInvalidData, i, p.Stride, rowBytes)
		}

		// 最小容量 = (rows-1)*stride + rowBytes：最后一行不要求补齐到 stride，
		// 这与 ffmpeg 的 av_image_fill_arrays 语义一致，避免调用方被迫多分配。
		need := rowBytes
		if h > 1 {
			span, ok := mulOK(h-1, p.Stride)
			if !ok {
				return fmt.Errorf("%w: plane %d row span overflows", ErrInvalidData, i)
			}
			need, ok = addOK(span, rowBytes)
			if !ok {
				return fmt.Errorf("%w: plane %d total size overflows", ErrInvalidData, i)
			}
		}
		if len(p.Data) < need {
			return fmt.Errorf("%w: plane %d needs %d bytes, has %d", ErrInvalidData, i, need, len(p.Data))
		}
		if bps == 2 {
			limit := uint16((1 << uint(f.Format.BitDepth())) - 1)
			for y := range h {
				row := p.Data[y*p.Stride : y*p.Stride+rowBytes]
				for x := range w {
					if binary.LittleEndian.Uint16(row[2*x:]) > limit {
						return fmt.Errorf("%w: plane %d sample (%d,%d) exceeds %d", ErrInvalidData, i, x, y, limit)
					}
				}
			}
		}
	}
	return nil
}

// mulOK 返回 a*b，并在结果为负或溢出时返回 false。
//
// 负数入参一律视为失败：尺寸类字段出现负值一定是错误输入，
// 继续计算只会把问题推到更远的地方。
func mulOK(a, b int) (int, bool) {
	if a < 0 || b < 0 {
		return 0, false
	}
	if a == 0 || b == 0 {
		return 0, true
	}
	if b > math.MaxInt/a {
		return 0, false
	}
	return a * b, true
}

// addOK 返回 a+b，并在溢出时返回 false（入参按无符号语义处理，负值视为失败）。
func addOK(a, b int) (int, bool) {
	if a < 0 || b < 0 {
		return 0, false
	}
	if b > math.MaxInt-a {
		return 0, false
	}
	return a + b, true
}
