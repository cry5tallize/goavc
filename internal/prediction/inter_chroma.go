package prediction

import (
	"github.com/goavc/h264/internal/picture"
)

// ChromaSample 按双线性插值读取一个色度预测样本（8.4.2.2.2）。
//
// cx/cy 是色度整像素坐标，mvx/mvy 仍是亮度四分之一像素偏移：
// 4:2:0 下 1 个色度像素 = 4 个亮度四分之一像素，故位置以 1/8
// 色度像素为单位，低 3 位是插值相位。
func ChromaSample(p *picture.Plane, cx, cy, mvx, mvy int) byte {
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
	return byte((a*p00 + b*p01 + c*p10 + d*p11 + 32) >> 6)
}

// MCBlockChroma 填满一个 w×h 的色度预测块。
func MCBlockChroma(dst []byte, dstStride int, ref *picture.Plane, cx, cy, w, h, mvx, mvy int) {
	for j := 0; j < h; j++ {
		row := dst[j*dstStride : j*dstStride+w]
		for i := 0; i < w; i++ {
			row[i] = ChromaSample(ref, cx+i, cy+j, mvx, mvy)
		}
	}
}
