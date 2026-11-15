// Package pattern 生成可复现的三平面测试图案，不依赖外部工具。
package pattern

import (
	h264 "github.com/cry5tallize/goavc"
	"math/rand"
)

// Frame 生成噪声、二维斜坡、水平/垂直斜坡、对角和分块六类图案。
// index 决定图案和固定随机种子；紧密布局方便直接计算输入 SHA-256。
func Frame(width, height, index int) *h264.Frame {
	f := &h264.Frame{Width: width, Height: height, Format: h264.YUV420P, PTS: int64(index), Duration: 1}
	rng := rand.New(rand.NewSource(int64(123 + index)))
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, width, height)
		p := h264.Plane{Data: make([]byte, w*h), Stride: w}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var v int
				switch index % 6 {
				case 0:
					v = rng.Intn(256)
				case 1:
					v = 80 + (x*3+y*2+index)%70
				case 2:
					v = 40 + (x*7+index)%170
				case 3:
					v = 40 + (y*7+index)%170
				case 4:
					v = 60 + (x-y+height+index)%120
				case 5:
					v = 40 + (x/4+y/4+index)%180
				}
				p.Data[y*w+x] = byte(v)
			}
		}
		f.Planes[c] = p
	}
	return f
}
