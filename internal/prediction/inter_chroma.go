package prediction

import (
	"github.com/goavc/h264/internal/picture"
)

// ChromaSample 按双线性插值读取一个色度预测样本（8.4.2.2.2）。
//
// cx/cy 是色度整像素坐标，mvx/mvy 仍是亮度四分之一像素偏移：
// 4:2:0 下 1 个色度像素 = 8 个亮度四分之一像素，故位置以 1/8
// 色度像素为单位，低 3 位是插值相位。
func ChromaSample(p *picture.Plane, cx, cy, mvx, mvy int) byte {
	return ChromaSampleFormat(p, cx, cy, mvx, mvy, 2, 2)
}

// ChromaSampleHigh 实现高位深 4:2:0 色度的全部 64 个双线性相位。
func ChromaSampleHigh(p *picture.Plane, cx, cy, mvx, mvy int) uint16 {
	return ChromaSampleFormatHigh(p, cx, cy, mvx, mvy, 2, 2)
}

// ChromaSampleFormat 按平面采样格式插值（Spec: 8.4.2.2）。
// subW/subH 为亮度与本平面的采样比：420=(2,2)、422=(2,1)、444=(1,1)。
// cx/cy 是本平面整像素坐标，MV 始终以亮度 1/4 像素计，调用方不得预缩放。
// 444 复用亮度六抽头；420/422 使用 1/8 水平与 1/(4*subH) 垂直双线性。
func ChromaSampleFormat(p *picture.Plane, cx, cy, mvx, mvy, subW, subH int) byte {
	ref := samplePlane[byte]{p.Data, p, 8}
	return chromaSampleFormat(&ref, cx, cy, mvx, mvy, subW, subH)
}

// ChromaSampleFormatHigh 在 Data16 上插值，BitDepth 控制六抽头最终裁剪。
func ChromaSampleFormatHigh(p *picture.Plane, cx, cy, mvx, mvy, subW, subH int) uint16 {
	ref := samplePlane[uint16]{p.Data16, p, p.BitDepth}
	return chromaSampleFormat(&ref, cx, cy, mvx, mvy, subW, subH)
}

func chromaSampleFormat[T sample](p *samplePlane[T], cx, cy, mvx, mvy, subW, subH int) T {
	if subW == 1 && subH == 1 {
		return lumaSample(p, cx, cy, mvx, mvy)
	}
	return chromaSample(p, cx, cy, mvx*(2/subW), mvy*(2/subH))
}

func chromaSample[T sample](p *samplePlane[T], cx, cy, mvx, mvy int) T {
	fullX := cx*8 + mvx
	fullY := cy*8 + mvy
	ix := fullX >> 3
	iy := fullY >> 3
	fx := fullX & 7
	fy := fullY & 7
	if fx == 0 && fy == 0 {
		return pixClamped(p, ix, iy)
	}
	a := (8 - fx) * (8 - fy)
	b := fx * (8 - fy)
	c := (8 - fx) * fy
	d := fx * fy
	p00 := int(pixClamped(p, ix, iy))
	p01 := int(pixClamped(p, ix+1, iy))
	p10 := int(pixClamped(p, ix, iy+1))
	p11 := int(pixClamped(p, ix+1, iy+1))
	return T((a*p00 + b*p01 + c*p10 + d*p11 + 32) >> 6)
}

// MCBlockChroma 填满一个 w×h 的色度预测块。
func MCBlockChroma(dst []byte, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy int) {
	MCBlockChromaFormat(dst, dstStride, ref, cx, cy, w, h, mvx, mvy, 2, 2)
}

// MCBlockChromaHigh 在原始高位深缓冲上填充 4:2:0 色度预测块。
func MCBlockChromaHigh(dst []uint16, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy int) {
	MCBlockChromaFormatHigh(dst, dstStride, ref, cx, cy, w, h, mvx, mvy, 2, 2)
}

// MCBlockChromaFormat 按 ChromaSampleFormat 的格式语义填充 w×h 色度块。
// dstStride、ref.Stride、位置与宽高均以本平面采样计；合法采样比由调用方保证。
func MCBlockChromaFormat(dst []byte, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy, subW, subH int) {
	p := samplePlane[byte]{ref.Data, ref, 8}
	mcBlockChromaFormat(dst, dstStride, &p, cx, cy, w, h, mvx, mvy, subW, subH)
}

// MCBlockChromaFormatHigh 直接读取 Data16，跨度仍以采样而非字节计。
func MCBlockChromaFormatHigh(dst []uint16, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy, subW, subH int) {
	p := samplePlane[uint16]{ref.Data16, ref, ref.BitDepth}
	mcBlockChromaFormat(dst, dstStride, &p, cx, cy, w, h, mvx, mvy, subW, subH)
}

func mcBlockChromaFormat[T sample](dst []T, dstStride int, ref *samplePlane[T], cx, cy, w, h, mvx, mvy, subW, subH int) {
	if subW == 1 && subH == 1 {
		mcBlockLuma(dst, dstStride, ref, cx, cy, w, h, mvx, mvy)
		return
	}
	// 先把 MV 变成 1/8 本平面采样，避免逐像素重复换算。
	mvx *= 2 / subW
	mvy *= 2 / subH
	for j := 0; j < h; j++ {
		row := dst[j*dstStride : j*dstStride+w]
		for i := 0; i < w; i++ {
			row[i] = chromaSample(ref, cx+i, cy+j, mvx, mvy)
		}
	}
}
