package prediction

import (
	"github.com/goavc/h264/internal/picture"
)

// LumaSample 按亮度 6-tap 插值读取一个预测样本（8.4.2.2.1）。
//
// x/y 是整像素基准位置，mvx/mvy 是四分之一像素偏移。整数位置与插值
// 所需的 -2..+3 邻域都按参考帧边界裁剪（边缘复制），因此调用方不需要
// 预先扩展边界。
//
// 插值分级：
//   - 半像素：6-tap 与 +16>>5，结果裁剪到 0..255；
//   - 四分之一像素：在整像素与半像素之间做 (a+b+1)>>1；
//   - 双半像素 j：中间值不裁剪，最终 +512>>10 再裁剪。
//
// 最后一点是 H.264 与“先裁剪再插值”的朴素实现最容易分歧的地方。
func LumaSample(p *picture.Plane, x, y, mvx, mvy int) byte {
	ref := samplePlane[byte]{p.Data, p, 8}
	return lumaSample(&ref, x, y, mvx, mvy)
}

// LumaSampleHigh 直接在 Data16 上插值，六抽头中间值使用有符号 int。
func LumaSampleHigh(p *picture.Plane, x, y, mvx, mvy int) uint16 {
	ref := samplePlane[uint16]{p.Data16, p, p.BitDepth}
	return lumaSample(&ref, x, y, mvx, mvy)
}

type samplePlane[T sample] struct {
	data     []T
	layout   *picture.Plane
	bitDepth int
}

func lumaSample[T sample](p *samplePlane[T], x, y, mvx, mvy int) T {
	fullX := x*4 + mvx
	fullY := y*4 + mvy
	lx := fullX >> 2
	ly := fullY >> 2
	fx := fullX & 3
	fy := fullY & 3

	if fx == 0 && fy == 0 {
		return pixClamped(p, lx, ly)
	}
	if fy == 0 {
		h := hSample(p, lx, ly)
		switch fx {
		case 1:
			return avg2(pixClamped(p, lx, ly), h)
		case 2:
			return h
		default: // 3
			return avg2(pixClamped(p, lx+1, ly), h)
		}
	}
	if fx == 0 {
		v := vSample(p, lx, ly)
		switch fy {
		case 1:
			return avg2(pixClamped(p, lx, ly), v)
		case 2:
			return v
		default: // 3
			return avg2(pixClamped(p, lx, ly+1), v)
		}
	}
	if fx == 2 && fy == 2 {
		return jSample(p, lx, ly)
	}
	if fx == 2 {
		if fy == 1 {
			return avg2(hSample(p, lx, ly), jSample(p, lx, ly))
		}
		return avg2(hSample(p, lx, ly+1), jSample(p, lx, ly))
	}
	if fy == 2 {
		if fx == 1 {
			return avg2(vSample(p, lx, ly), jSample(p, lx, ly))
		}
		return avg2(vSample(p, lx+1, ly), jSample(p, lx, ly))
	}
	// 四个角上的四分之一像素：两个半像素平均。
	switch {
	case fx == 1 && fy == 1:
		return avg2(hSample(p, lx, ly), vSample(p, lx, ly))
	case fx == 3 && fy == 1:
		return avg2(hSample(p, lx, ly), vSample(p, lx+1, ly))
	case fx == 1 && fy == 3:
		return avg2(hSample(p, lx, ly+1), vSample(p, lx, ly))
	default: // 3,3
		return avg2(hSample(p, lx, ly+1), vSample(p, lx+1, ly))
	}
}

// MCBlockLuma 用 LumaSample 填满一个 w×h 的亮度预测块。
func MCBlockLuma(dst []byte, dstStride int, ref *picture.Plane, x, y, w, h, mvx, mvy int) {
	p := samplePlane[byte]{ref.Data, ref, 8}
	mcBlockLuma(dst, dstStride, &p, x, y, w, h, mvx, mvy)
}

// MCBlockLumaHigh 的位置、跨度与宽高均以采样计。
func MCBlockLumaHigh(dst []uint16, dstStride int, ref *picture.Plane, x, y, w, h, mvx, mvy int) {
	p := samplePlane[uint16]{ref.Data16, ref, ref.BitDepth}
	mcBlockLuma(dst, dstStride, &p, x, y, w, h, mvx, mvy)
}

func mcBlockLuma[T sample](dst []T, dstStride int, ref *samplePlane[T], x, y, w, h, mvx, mvy int) {
	for j := 0; j < h; j++ {
		row := dst[j*dstStride : j*dstStride+w]
		for i := 0; i < w; i++ {
			row[i] = lumaSample(ref, x+i, y+j, mvx, mvy)
		}
	}
}

// hSample 是亮度水平半像素插值。
//
// (a - 5b + 20c + 20d - 5e + f + 16) >> 5，再 Clip1Y。
func hSample[T sample](p *samplePlane[T], x, y int) T {
	s0 := int(pixClamped(p, x-2, y))
	s1 := int(pixClamped(p, x-1, y))
	s2 := int(pixClamped(p, x, y))
	s3 := int(pixClamped(p, x+1, y))
	s4 := int(pixClamped(p, x+2, y))
	s5 := int(pixClamped(p, x+3, y))
	return clipSample[T]((s0-5*s1+20*s2+20*s3-5*s4+s5+16)>>5, p.bitDepth)
}

// vSample 是亮度垂直半像素插值。
func vSample[T sample](p *samplePlane[T], x, y int) T {
	s0 := int(pixClamped(p, x, y-2))
	s1 := int(pixClamped(p, x, y-1))
	s2 := int(pixClamped(p, x, y))
	s3 := int(pixClamped(p, x, y+1))
	s4 := int(pixClamped(p, x, y+2))
	s5 := int(pixClamped(p, x, y+3))
	return clipSample[T]((s0-5*s1+20*s2+20*s3-5*s4+s5+16)>>5, p.bitDepth)
}

// jSample 是亮度双半像素插值。
//
// 先对 6 行做不裁剪的水平 6-tap，再对 6 个中间值做垂直 6-tap，
// 最后 +512>>10。中间值刻意不裁剪，这是规范 8.4.2.2.1 的要求。
func jSample[T sample](p *samplePlane[T], x, y int) T {
	var tmp [6]int
	for i := 0; i < 6; i++ {
		yy := y - 2 + i
		s0 := int(pixClamped(p, x-2, yy))
		s1 := int(pixClamped(p, x-1, yy))
		s2 := int(pixClamped(p, x, yy))
		s3 := int(pixClamped(p, x+1, yy))
		s4 := int(pixClamped(p, x+2, yy))
		s5 := int(pixClamped(p, x+3, yy))
		tmp[i] = s0 - 5*s1 + 20*s2 + 20*s3 - 5*s4 + s5
	}
	sum := tmp[0] - 5*tmp[1] + 20*tmp[2] + 20*tmp[3] - 5*tmp[4] + tmp[5]
	return clipSample[T]((sum+512)>>10, p.bitDepth)
}

func pixClamped[T sample](p *samplePlane[T], x, y int) T {
	if x < 0 {
		x = 0
	} else if x >= p.layout.Width {
		x = p.layout.Width - 1
	}
	if y < 0 {
		y = 0
	} else if y >= p.layout.Height {
		y = p.layout.Height - 1
	}
	return p.data[(y+p.layout.Border)*p.layout.Stride+x+p.layout.Border]
}
