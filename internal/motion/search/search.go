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

type window struct {
	original, candidate [256]byte
	ref                 *picture.Plane
	x, y, w, h, radius  int
	best                MV
	cost                int
}

func newWindow(src, ref *picture.Plane, x, y, w, h, radius int) window {
	s := window{ref: ref, x: x, y: y, w: w, h: h, radius: min(max(1, radius), 64, max(4, min(ref.Width, ref.Height)/4)), cost: int(^uint(0) >> 1)}
	for j := 0; j < h; j++ {
		copy(s.original[j*16:j*16+w], src.Row(y + j)[x:x+w])
	}
	return s
}

func (s *window) consider(m MV) {
	if m.X < int32(-s.radius*4) || m.X > int32(s.radius*4) || m.Y < int32(-s.radius*4) || m.Y > int32(s.radius*4) {
		return
	}
	var cost int
	rx, ry := s.x+int(m.X)/4, s.y+int(m.Y)/4
	if m.X%4 == 0 && m.Y%4 == 0 && rx >= 0 && ry >= 0 && rx+s.w <= s.ref.Width && ry+s.h <= s.ref.Height {
		// 整像素代价直接比较原始采样；边界外推和亚像素仍只由共享 MC 定义。
		offset := s.ref.Index(rx, ry)
		cost = SAD(s.original[:], 16, s.ref.Data[offset:], s.ref.Stride, s.w, s.h)
	} else {
		prediction.MCBlockLuma(s.candidate[:], 16, s.ref, s.x, s.y, s.w, s.h, int(m.X), int(m.Y))
		cost = SAD(s.original[:], 16, s.candidate[:], 16, s.w, s.h)
	}
	if cost < s.cost {
		s.best = m
		s.cost = cost
	}
}

// Integer 对最大 16×16 区域做整像素估计。radius=1..64，小图像自适应收缩。
// full 选择穷举，否则从零与 MVP 中较优点执行严格下降的有限菱形搜索。
func Integer(src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool) (MV, int) {
	s := newWindow(src, ref, x, y, w, h, radius)
	s.consider(MV{})
	s.consider(MV{X: int32(min(s.radius, max(-s.radius, int(predictor.X)/4)) * 4), Y: int32(min(s.radius, max(-s.radius, int(predictor.Y)/4)) * 4)})
	if full {
		for dy := -s.radius; dy <= s.radius; dy++ {
			for dx := -s.radius; dx <= s.radius; dx++ {
				s.consider(MV{X: int32(dx * 4), Y: int32(dy * 4)})
			}
		}
	} else {
		for {
			center := s.best
			for _, d := range [...]MV{{X: -4}, {X: 4}, {Y: -4}, {Y: 4}} {
				s.consider(MV{X: center.X + d.X, Y: center.Y + d.Y})
			}
			if s.best == center {
				break
			}
		}
	}
	return s.best, s.cost
}

// Refine 使用共享规范 MC 对给定整像素点做半/四像素细化。
// 可以在分区选择之后调用，避免为被丢弃的分区重复计算六抽头插值。
func Refine(src, ref *picture.Plane, x, y, w, h int, base MV, radius int) (MV, int) {
	s := newWindow(src, ref, x, y, w, h, radius)
	s.consider(base)
	for _, step := range []int32{2, 1} {
		center := s.best
		for dy := -step; dy <= step; dy += step {
			for dx := -step; dx <= step; dx += step {
				s.consider(MV{X: center.X + dx, Y: center.Y + dy})
			}
		}
	}
	return s.best, s.cost
}

// Estimate 组合整像素搜索与亚像素细化，供独立小块搜索和验证使用。
func Estimate(src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool) (MV, int) {
	base, _ := Integer(src, ref, x, y, w, h, predictor, radius, full)
	return Refine(src, ref, x, y, w, h, base, radius)
}
