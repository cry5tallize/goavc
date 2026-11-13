// Package search 提供编码器运动估计；独立于 motion 的共享 MV 内核，
// 避免 prediction 的加权距离推导与搜索之间产生反向依赖。
package search

import (
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
)

type MV = motion.MV

// SAD 返回有效矩形的绝对差和。调用方须先验证缓冲/stride；只读有效像素，
// 不读取行尾填充，所以支持奇数尺寸与非对齐 stride。
func SAD(a []byte, as int, b []byte, bs, width, height int) int {
	sum := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			d := int(a[y*as+x]) - int(b[y*bs+x])
			sum += max(d, -d)
		}
	}
	return sum
}

// Estimate 对最大 16×16 区域做整像素估计与半/四分之一像素细化。
// radius 范围 1..64；small picture 自适应缩小范围，MV 以零位移为中心限界。
// full 选择穷举，否则从零与 MVP 中较优点执行菱形搜索。预测只调用共享 MC，
// 图像边界由 MC 的边缘复制处理，不读取未分配的外部缓冲。
func Estimate(src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool) (MV, int) {
	radius = min(max(1, radius), 64, max(4, min(ref.Width, ref.Height)/4))
	var original, candidate [256]byte
	for j := 0; j < h; j++ {
		copy(original[j*16:j*16+w], src.Row(y + j)[x:x+w])
	}
	best := MV{}
	cost := int(^uint(0) >> 1)
	score := func(m MV) int {
		prediction.MCBlockLuma(candidate[:], 16, ref, x, y, w, h, int(m.X), int(m.Y))
		return SAD(original[:], 16, candidate[:], 16, w, h)
	}
	consider := func(m MV) bool {
		if m.X < int32(-radius*4) || m.X > int32(radius*4) || m.Y < int32(-radius*4) || m.Y > int32(radius*4) {
			return false
		}
		v := score(m)
		if v < cost {
			cost = v
			best = m
			return true
		}
		return false
	}
	consider(MV{})
	seed := MV{X: int32(min(radius, max(-radius, int(predictor.X)/4)) * 4), Y: int32(min(radius, max(-radius, int(predictor.Y)/4)) * 4)}
	consider(seed)
	if full {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				consider(MV{X: int32(dx * 4), Y: int32(dy * 4)})
			}
		}
	} else {
		// 严格下降才能继续；最多搜索有限网格上的点，不会在相等代价平台循环。
		for {
			center := best
			for _, d := range [...]MV{{X: -4}, {X: 4}, {Y: -4}, {Y: 4}} {
				consider(MV{X: center.X + d.X, Y: center.Y + d.Y})
			}
			if best == center {
				break
			}
		}
	}
	for _, step := range []int32{2, 1} {
		center := best
		for dy := -step; dy <= step; dy += step {
			for dx := -step; dx <= step; dx += step {
				consider(MV{X: center.X + dx, Y: center.Y + dy})
			}
		}
	}
	return best, cost
}
