package prediction

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
)

type Intra8x8Samples = intra8x8Samples[byte]

// Intra8x8Samples16 是高位深 8×8 邻域，保留独立左上可用性。
type Intra8x8Samples16 = intra8x8Samples[uint16]

type intra8x8Samples[T sample] struct {
	Top              [16]T
	Left             [8]T
	TopLeft          T
	TopLeftAvailable bool
}

// Intra8x8 实现 Spec: 8.3.2 的邻域三抽头平滑及九种方向。右上不可用时
// 调用方先以 Top[7] 扩展 Top[8..15]；TopLeft 有独立可用性，不能从
// “上和左都可用”推断，尤其是 slice 起点与 constrained intra 边界。
func Intra8x8(dst []byte, stride, mode int, s Intra8x8Samples, topAvail, leftAvail bool) error {
	return intra8x8(dst, stride, mode, s, topAvail, leftAvail, 8)
}

// Intra8x8High 以采样为跨度，共用邻域平滑和九方向算术。
func Intra8x8High(dst []uint16, stride, mode int, s Intra8x8Samples16, topAvail, leftAvail bool, bitDepth int) error {
	if err := checkBitDepth(bitDepth); err != nil {
		return err
	}
	return intra8x8(dst, stride, mode, s, topAvail, leftAvail, bitDepth)
}

func intra8x8[T sample](dst []T, stride, mode int, s intra8x8Samples[T], topAvail, leftAvail bool, bitDepth int) error {
	if stride < 8 || len(dst) < 7*stride+8 || mode < 0 || mode > 8 {
		return fmt.Errorf("prediction: invalid intra8x8 arguments: %w", h264.ErrInvalidData)
	}
	if (mode == 0 || mode == 3 || mode == 7) && !topAvail {
		return fmt.Errorf("prediction: intra8x8 top unavailable: %w", h264.ErrInvalidData)
	}
	if (mode == 1 || mode == 8) && !leftAvail {
		return fmt.Errorf("prediction: intra8x8 left unavailable: %w", h264.ErrInvalidData)
	}
	if (mode == 4 || mode == 5 || mode == 6) && (!topAvail || !leftAvail || !s.TopLeftAvailable) {
		return fmt.Errorf("prediction: intra8x8 diagonal neighborhood unavailable: %w", h264.ErrInvalidData)
	}
	var top [16]T
	var left [8]T
	tl := s.TopLeft
	if topAvail {
		for i := 0; i < 16; i++ {
			if i == 0 {
				a := s.Top[0]
				if s.TopLeftAvailable {
					a = s.TopLeft
				}
				top[i] = avg3(a, s.Top[0], s.Top[1])
			} else {
				top[i] = avg3(s.Top[i-1], s.Top[i], s.Top[min(15, i+1)])
			}
		}
	}
	if leftAvail {
		for i := 0; i < 8; i++ {
			if i == 0 {
				a := s.Left[0]
				if s.TopLeftAvailable {
					a = s.TopLeft
				}
				left[i] = avg3(a, s.Left[0], s.Left[1])
			} else {
				left[i] = avg3(s.Left[i-1], s.Left[i], s.Left[min(7, i+1)])
			}
		}
	}
	if s.TopLeftAvailable {
		switch {
		case topAvail && leftAvail:
			tl = avg3(s.Left[0], s.TopLeft, s.Top[0])
		case topAvail:
			tl = avg3(s.TopLeft, s.TopLeft, s.Top[0])
		case leftAvail:
			tl = avg3(s.Left[0], s.TopLeft, s.TopLeft)
		}
	}
	t := func(i int) T {
		if i < 0 {
			return tl
		}
		return top[min(15, i)]
	}
	l := func(i int) T {
		if i < 0 {
			return tl
		}
		return left[min(7, i)]
	}
	vr := func(x, y int, topRef, leftRef func(int) T) T {
		z := 2*x - y
		if z >= 0 {
			if z%2 == 0 {
				return avg2(topRef(z/2-1), topRef(z/2))
			}
			u := (z - 1) / 2
			return avg3(topRef(u-1), topRef(u), topRef(u+1))
		}
		if z == -1 {
			return avg3(leftRef(0), tl, topRef(0))
		}
		k := -z
		return avg3(leftRef(k-3), leftRef(k-2), leftRef(k-1))
	}
	dc := T(1 << (bitDepth - 1))
	if mode == 2 {
		sum, n := 0, 0
		if topAvail {
			for _, v := range top[:8] {
				sum += int(v)
			}
			n += 8
		}
		if leftAvail {
			for _, v := range left {
				sum += int(v)
			}
			n += 8
		}
		if n > 0 {
			dc = T((sum + n/2) / n)
		}
	}
	for y := 0; y < 8; y++ {
		row := dst[y*stride : y*stride+8]
		for x := 0; x < 8; x++ {
			var v T
			switch mode {
			case 0:
				v = top[x]
			case 1:
				v = left[y]
			case 2:
				v = dc
			case 3:
				v = avg3(t(x+y), t(x+y+1), t(x+y+2))
			case 4:
				d := x - y
				if d > 0 {
					v = avg3(t(d-2), t(d-1), t(d))
				} else if d < 0 {
					k := -d
					v = avg3(l(k-2), l(k-1), l(k))
				} else {
					v = avg3(left[0], tl, top[0])
				}
			case 5:
				v = vr(x, y, t, l)
			case 6:
				v = vr(y, x, l, t)
			case 7:
				k := x + y/2
				if y%2 == 0 {
					v = avg2(t(k), t(k+1))
				} else {
					v = avg3(t(k), t(k+1), t(k+2))
				}
			case 8:
				k := y + x/2
				if x%2 == 0 {
					v = avg2(l(k), l(k+1))
				} else {
					v = avg3(l(k), l(k+1), l(k+2))
				}
			}
			row[x] = v
		}
	}
	return nil
}
