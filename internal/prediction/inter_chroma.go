package prediction

import (
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/simd"
)

// ChromaSample 的MV仍以亮度1/4像素计，420色度为1/8本平面采样。
func ChromaSample(p *picture.Plane, cx, cy, mvx, mvy int) byte {
	return ChromaSampleFormat(p, cx, cy, mvx, mvy, 2, 2)
}

// ChromaSampleHigh 直接读取原生10位采样，不借用公共LE存储视图。
func ChromaSampleHigh(p *picture.Plane, cx, cy, mvx, mvy int) uint16 {
	return ChromaSampleFormatHigh(p, cx, cy, mvx, mvy, 2, 2)
}

// ChromaSampleFormat 保持420/422双线性与444六抽头的格式语义（Spec: 8.4.2.2）。
func ChromaSampleFormat(p *picture.Plane, cx, cy, mvx, mvy, subW, subH int) byte {
	var out [1]byte
	MCBlockChromaFormat(out[:], 1, p, cx, cy, 1, 1, mvx, mvy, subW, subH)
	return out[0]
}

// ChromaSampleFormatHigh 与块入口共享完整位深、场寻址和舍入。
func ChromaSampleFormatHigh(p *picture.Plane, cx, cy, mvx, mvy, subW, subH int) uint16 {
	var out [1]uint16
	MCBlockChromaFormatHigh(out[:], 1, p, cx, cy, 1, 1, mvx, mvy, subW, subH)
	return out[0]
}

// MCBlockChroma 填充420色度块，stride和宽高以本平面采样计。
func MCBlockChroma(dst []byte, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy int) {
	MCBlockChromaFormat(dst, dstStride, ref, cx, cy, w, h, mvx, mvy, 2, 2)
}

// MCBlockChromaHigh 是420的原生高位深入口。
func MCBlockChromaHigh(dst []uint16, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy int) {
	MCBlockChromaFormatHigh(dst, dstStride, ref, cx, cy, w, h, mvx, mvy, 2, 2)
}

// MCBlockChromaFormat 的采样比由调用者校验；444必须复用亮度六抽头。
func MCBlockChromaFormat(dst []byte, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy, subW, subH int) {
	p := samplePlane[byte]{ref.Data, ref, 8}
	mcBlockChromaFormat(dst, dstStride, &p, cx, cy, w, h, mvx, mvy, subW, subH)
}

// MCBlockChromaFormatHigh 的stride仍以uint16采样计，不在领域层乘2。
func MCBlockChromaFormatHigh(dst []uint16, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy, subW, subH int) {
	p := samplePlane[uint16]{ref.Data16, ref, ref.BitDepth}
	mcBlockChromaFormat(dst, dstStride, &p, cx, cy, w, h, mvx, mvy, subW, subH)
}

func mcBlockChromaFormat[T sample](dst []T, dstStride int, ref *samplePlane[T], cx, cy, w, h, mvx, mvy, subW, subH int) {
	mcBlockChromaMode(dst, dstStride, ref, cx, cy, w, h, mvx, mvy, subW, subH, false)
}

// MCBlockChromaFormatAverage保留色度相位映射和逻辑边缘，融合最终均值。
//
//go:noinline
func MCBlockChromaFormatAverage(dst []byte, ds int, ref *picture.Plane, x, y, w, h, mvx, mvy, subW, subH int) {
	p := samplePlane[byte]{ref.Data, ref, 8}
	mcBlockChromaMode(dst, ds, &p, x, y, w, h, mvx, mvy, subW, subH, true)
}

func mcBlockChromaMode[T sample](dst []T, dstStride int, ref *samplePlane[T], cx, cy, w, h, mvx, mvy, subW, subH int, average bool) {
	if subW == 1 && subH == 1 {
		mcBlockLumaMode(dst, dstStride, ref, cx, cy, w, h, mvx, mvy, average)
		return
	}
	fullX, fullY := cx*8+mvx*(2/subW), cy*8+mvy*(2/subH)
	if mcCoordinateWraps(fullX, fullY, w, h, 8) {
		for j := range h {
			row := dst[j*dstStride:][:w]
			for i := range w {
				var one [1]T
				mcBlockChromaFormat(one[:], 1, ref, cx+i, cy+j, 1, 1, mvx, mvy, subW, subH)
				v := one[0]
				if average {
					v = T((int(row[i]) + int(v) + 1) >> 1)
				}
				row[i] = v
			}
		}
		return
	}
	ix, iy, fx, fy := fullX>>3, fullY>>3, fullX&7, fullY&7
	right, bottom := 0, 0
	if fx != 0 {
		right = 1
	}
	if fy != 0 {
		bottom = 1
	}
	var patch [21 * 21]T
	for by := 0; by < h; by += 16 {
		th := min(16, h-by)
		for bx := 0; bx < w; bx += 16 {
			tw := min(16, w-bx)
			source, stride := mcPatch(ref, ix+bx, iy+by, tw+right, th+bottom, &patch)
			if average {
				simd.MCChromaAverage8(any(dst[by*dstStride+bx:]).([]byte), dstStride, any(source).([]byte), stride, tw, th, fx, fy)
			} else {
				simd.MCChroma(dst[by*dstStride+bx:], dstStride, source, stride, tw, th, fx, fy)
			}
		}
	}
}
