// Package search 提供编码器运动估计；独立于 motion 的共享 MV 内核，
// 避免 prediction 的加权距离推导与搜索之间产生反向依赖。
package search

import (
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
	"github.com/goavc/h264/internal/simd"
)

type MV = motion.MV

// Scorer 将运动矢量和像素 SAD 映射为编码代价；由上层提供比特模型，
// 搜索包不依赖编码流水线。nil 表示只比较 SAD。
type Scorer func(MV, int) uint64

// SAD 返回有效矩形的绝对差和。调用方须先验证缓冲/stride；只读有效像素，
// 不读取行尾填充，所以支持奇数尺寸与非对齐 stride。
func SAD(a []byte, as int, b []byte, bs, width, height int) int {
	return simd.SAD8(a, as, b, bs, width, height)
}

// sample 仅覆盖平面的两种原生存储；SAD 保留原位深，不缩放像素差。
type sample interface {
	byte | uint16
}

type window[T sample] struct {
	original, candidate [256]T
	reference           []T
	ref                 *picture.Plane
	x, y, w, h, radius  int
	best                MV
	cost                uint64
	sad                 int
	scorer              Scorer
	sadKernel           func([]T, int, []T, int, int, int) int
}

func newWindow[T sample](src, ref []T, srcPlane, refPlane *picture.Plane, x, y, w, h, radius int) window[T] {
	s := window[T]{reference: ref, ref: refPlane, x: x, y: y, w: w, h: h, radius: min(max(1, radius), 64, max(4, min(refPlane.Width, refPlane.Height)/4)), cost: ^uint64(0)}
	// 每个搜索上下文仅绑定一次样本类型；候选/像素循环不再做类型判断。
	var zero T
	switch any(zero).(type) {
	case byte:
		s.sadKernel = any(simd.BindSAD8()).(func([]T, int, []T, int, int, int) int)
	case uint16:
		s.sadKernel = any(simd.BindSAD16()).(func([]T, int, []T, int, int, int) int)
	}
	for j := range h {
		offset := srcPlane.Index(x, y+j)
		copy(s.original[j*16:j*16+w], src[offset:offset+w])
	}
	return s
}

func (s *window[T]) consider(m MV) {
	if m.X < int32(-s.radius*4) || m.X > int32(s.radius*4) || m.Y < int32(-s.radius*4) || m.Y > int32(s.radius*4) {
		return
	}
	var cost int
	rx, ry := s.x+int(m.X)/4, s.y+int(m.Y)/4
	if m.X%4 == 0 && m.Y%4 == 0 && rx >= 0 && ry >= 0 && rx+s.w <= s.ref.Width && ry+s.h <= s.ref.Height {
		// 整像素代价直接比较原始采样；边界外推和亚像素仍只由共享 MC 定义。
		offset := s.ref.Index(rx, ry)
		cost = s.sadKernel(s.original[:], 16, s.reference[offset:], s.ref.Stride, s.w, s.h)
	} else {
		// 只在插值入口区分存储类型；搜索边界、SAD 与代价决策完全共享。
		switch dst := any(s.candidate[:]).(type) {
		case []byte:
			prediction.MCBlockLuma(dst, 16, s.ref, s.x, s.y, s.w, s.h, int(m.X), int(m.Y))
		case []uint16:
			prediction.MCBlockLumaHigh(dst, 16, s.ref, s.x, s.y, s.w, s.h, int(m.X), int(m.Y))
		}
		cost = s.sadKernel(s.original[:], 16, s.candidate[:], 16, s.w, s.h)
	}
	rank := uint64(cost)
	if s.scorer != nil {
		rank = s.scorer(m, cost)
	}
	if rank < s.cost {
		s.best = m
		s.cost = rank
		s.sad = cost
	}
}

// Integer 对最大 16×16 区域做整像素估计。radius=1..64，小图像自适应收缩。
// full 选择穷举，否则从零与 MVP 中较优点执行严格下降的有限菱形搜索。
func Integer(src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool) (MV, int) {
	return integerWithCost(src, ref, x, y, w, h, predictor, radius, full, nil)
}

func integerWithCost(src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool, scorer Scorer) (MV, int) {
	if src.Data16 != nil {
		return integerSamples(src.Data16, ref.Data16, src, ref, x, y, w, h, predictor, radius, full, scorer)
	}
	return integerSamples(src.Data, ref.Data, src, ref, x, y, w, h, predictor, radius, full, scorer)
}

func integerSamples[T sample](srcSamples, refSamples []T, src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool, scorer Scorer) (MV, int) {
	s := newWindow(srcSamples, refSamples, src, ref, x, y, w, h, radius)
	s.scorer = scorer
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
	return s.best, s.sad
}

// Refine 使用共享规范 MC 对给定整像素点做半/四像素细化。
// 可以在分区选择之后调用，避免为被丢弃的分区重复计算六抽头插值。
func Refine(src, ref *picture.Plane, x, y, w, h int, base MV, radius int) (MV, int) {
	return refineWithCost(src, ref, x, y, w, h, base, radius, nil)
}

func refineWithCost(src, ref *picture.Plane, x, y, w, h int, base MV, radius int, scorer Scorer) (MV, int) {
	if src.Data16 != nil {
		return refineSamples(src.Data16, ref.Data16, src, ref, x, y, w, h, base, radius, scorer)
	}
	return refineSamples(src.Data, ref.Data, src, ref, x, y, w, h, base, radius, scorer)
}

func refineSamples[T sample](srcSamples, refSamples []T, src, ref *picture.Plane, x, y, w, h int, base MV, radius int, scorer Scorer) (MV, int) {
	s := newWindow(srcSamples, refSamples, src, ref, x, y, w, h, radius)
	s.scorer = scorer
	s.consider(base)
	for _, step := range []int32{2, 1} {
		center := s.best
		for dy := -step; dy <= step; dy += step {
			for dx := -step; dx <= step; dx += step {
				s.consider(MV{X: center.X + dx, Y: center.Y + dy})
			}
		}
	}
	return s.best, s.sad
}

// Estimate 组合整像素搜索与亚像素细化，供独立小块搜索和验证使用。
func Estimate(src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool) (MV, int) {
	base, _ := Integer(src, ref, x, y, w, h, predictor, radius, full)
	return Refine(src, ref, x, y, w, h, base, radius)
}

// EstimateWithCost 在整像素与亚像素两阶段一致使用上层代价，返回原始 SAD。
func EstimateWithCost(src, ref *picture.Plane, x, y, w, h int, predictor MV, radius int, full bool, scorer Scorer) (MV, int) {
	base, _ := integerWithCost(src, ref, x, y, w, h, predictor, radius, full, scorer)
	return refineWithCost(src, ref, x, y, w, h, base, radius, scorer)
}
