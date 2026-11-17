package prediction

import (
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/simd"
)

// LumaSample 按亮度六抽头和四分之一相位图读取单个样本（Spec: 8.4.2.2.1）。
// 与块入口共用唯一算术，所需halo按逻辑图像/场边界复制，不读取另一场。
func LumaSample(p *picture.Plane, x, y, mvx, mvy int) byte {
	ref := samplePlane[byte]{p.Data, p, 8}
	return lumaSample(&ref, x, y, mvx, mvy)
}

// LumaSampleHigh 保留原生10位低两位及带符号、未裁剪的六抽头中间值。
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
	var out [1]T
	mcBlockLuma(out[:], 1, p, x, y, 1, 1, mvx, mvy)
	return out[0]
}

// MCBlockLuma 填满w×h亮度预测块；位置、跨度以采样计，MV以1/4亮度像素计。
func MCBlockLuma(dst []byte, dstStride int, ref *picture.Plane, x, y, w, h, mvx, mvy int) {
	p := samplePlane[byte]{ref.Data, ref, 8}
	mcBlockLuma(dst, dstStride, &p, x, y, w, h, mvx, mvy)
}

// MCBlockLumaHigh 与byte路径共享块分解、边界和全部相位，不缩减位深。
func MCBlockLumaHigh(dst []uint16, dstStride int, ref *picture.Plane, x, y, w, h, mvx, mvy int) {
	p := samplePlane[uint16]{ref.Data16, ref, ref.BitDepth}
	mcBlockLuma(dst, dstStride, &p, x, y, w, h, mvx, mvy)
}

func mcBlockLuma[T sample](dst []T, dstStride int, ref *samplePlane[T], x, y, w, h, mvx, mvy int) {
	mcBlockLumaMode(dst, dstStride, ref, x, y, w, h, mvx, mvy, false)
}

// MCBlockLumaAverage与Put入口共用相位/边界适配，插值裁剪后直接平均目标。
//
//go:noinline
func MCBlockLumaAverage(dst []byte, ds int, ref *picture.Plane, x, y, w, h, mvx, mvy int) {
	p := samplePlane[byte]{ref.Data, ref, 8}
	mcBlockLumaMode(dst, ds, &p, x, y, w, h, mvx, mvy, true)
}

func mcBlockLumaMode[T sample](dst []T, dstStride int, ref *samplePlane[T], x, y, w, h, mvx, mvy int, average bool) {
	fullX, fullY := x*4+mvx, y*4+mvy
	if mcCoordinateWraps(fullX, fullY, w, h, 4) {
		// 罕见宽坐标仍保持旧入口的逐样本int回绕语义；正常视频走块快路。
		// 单样本递归的w/h均为1，必不再次进入此分支；插值算术仍只有一份。
		for j := range h {
			row := dst[j*dstStride:][:w]
			for i := range w {
				v := lumaSample(ref, x+i, y+j, mvx, mvy)
				if average {
					v = T((int(row[i]) + int(v) + 1) >> 1)
				}
				row[i] = v
			}
		}
		return
	}
	ix, iy, fx, fy := fullX>>2, fullY>>2, fullX&3, fullY&3
	left, right, top, bottom := 0, 0, 0, 0
	if fx != 0 {
		left, right = 2, 3
	}
	if fy != 0 {
		top, bottom = 2, 3
	}
	var patch [21 * 21]T
	// 大于内部小块上限的合法调用分块，不返回新的Unsupported，也不越固定scratch。
	for by := 0; by < h; by += 16 {
		th := min(16, h-by)
		for bx := 0; bx < w; bx += 16 {
			tw := min(16, w-bx)
			source, stride := mcPatch(ref, ix+bx-left, iy+by-top, tw+left+right, th+top+bottom, &patch)
			if average {
				simd.MCLumaAverage8(any(dst[by*dstStride+bx:]).([]byte), dstStride, any(source).([]byte), stride, tw, th, fx, fy)
			} else {
				simd.MCLuma(dst[by*dstStride+bx:], dstStride, source, stride, tw, th, fx, fy, ref.bitDepth)
			}
		}
	}
}

func mcCoordinateWraps(x, y, w, h, scale int) bool {
	limit := int(^uint(0) >> 1)
	return w > 0 && (w-1 > limit/scale || x > limit-(w-1)*scale) ||
		h > 0 && (h-1 > limit/scale || y > limit-(h-1)*scale)
}

// mcPatch只做寻址和边界适配，插值算术唯一位于simd。内部块借用原缓冲，
// 边缘块在有界scratch中复制最近逻辑采样；FieldView的stride/height天然生效。
func mcPatch[T sample](ref *samplePlane[T], x, y, w, h int, patch *[21 * 21]T) ([]T, int) {
	p := ref.layout
	if x >= 0 && y >= 0 && x <= p.Width-w && y <= p.Height-h {
		return ref.data[p.Index(x, y):], p.Stride
	}
	for j := range h {
		yy := min(max(y+j, 0), p.Height-1)
		row := ref.data[p.Index(0, yy):][:p.Width]
		for i := range w {
			patch[j*w+i] = row[min(max(x+i, 0), p.Width-1)]
		}
	}
	return patch[:w*h], w
}
