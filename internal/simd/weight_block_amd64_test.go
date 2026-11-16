//go:build amd64 && !purego

package simd

import (
	"fmt"
	"testing"
)

// 独立按规范int64解析式计算，不复用SIMD预处理的bias/offset。
func weightBlockAnalytic(a, b int64, weights, offsets [2]int32, denom uint32, depth int, both bool) int64 {
	round := int64(0)
	shift := denom
	off := int64(offsets[0]) << (depth - 8)
	if both {
		round = 1 << denom
		shift++
		off = ((((int64(offsets[0]) + int64(offsets[1])) << (depth - 8)) + 1) >> 1)
	} else if denom > 0 {
		round = 1 << (denom - 1)
	}
	sum := a*int64(weights[0]) + round
	if both {
		sum += b * int64(weights[1])
	}
	return min(int64(1<<depth-1), max(int64(0), (sum>>shift)+off))
}

func TestWeightBlockSSE2_ArithmeticAndBounds(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU没有SSE2")
	}
	for _, depth := range []int{8, 10} {
		for _, both := range []bool{false, true} {
			for denom := uint32(0); denom <= 7; denom++ {
				for _, w0 := range []int32{-128, -127, -64, -1, 0, 1, 32, 64, 127, 128} {
					for _, off := range []int32{-128, -1, 0, 1, 127} {
						weights, offsets := [2]int32{w0, -w0}, [2]int32{off, -off - 1}
						// 保证第二权重在规范域内，覆盖正负和极值。
						if !both {
							weights[1], offsets[1] = 0, 0
						}
						p, ok := prepareBlockWeight(weights, offsets, denom, depth, both, depth == 10)
						if !ok {
							t.Fatal("规范参数被拒绝")
						}
						var a, b [22]byte
						var u, v [22]uint16
						var got, want [24]byte
						var high, expected [24]uint16
						for i := range a {
							a[i], b[i] = byte(i*71), byte(255-i*23)
							u[i], v[i] = uint16(i*3211), uint16(65535-i*131)
						}
						u[1], u[2], u[3], u[4] = 0, 1023, 32768, 65535
						for i := range got {
							got[i], want[i] = 77, 77
							high[i], expected[i] = 777, 777
						}
						if depth == 8 {
							second := b[1:]
							if !both {
								second = nil
							}
							weightBlock8SSE2(got[1:], 9, a[1:], second, 7, 4, 3, p.coeff, p.bias, p.offset, p.shift)
						} else {
							second := v[1:]
							if !both {
								second = nil
							}
							weightBlock16SSE2(high[1:], 9, u[1:], second, 7, 4, 3, p.coeff, p.bias, p.offset, p.shift, 1023)
						}
						for y := range 3 {
							for x := range 4 {
								i, j := 1+y*9+x, 1+y*7+x
								want[i] = byte(weightBlockAnalytic(int64(a[j]), int64(b[j]), weights, offsets, denom, depth, both))
								expected[i] = uint16(weightBlockAnalytic(int64(u[j]), int64(v[j]), weights, offsets, denom, depth, both))
							}
						}
						if depth == 8 && got != want || depth == 10 && high != expected {
							t.Fatalf("depth%d both%t denom%d weights%v offsets%v不符或越界写", depth, both, denom, weights, offsets)
						}
					}
				}
			}
		}
	}
}

func TestGuardPage_WeightBlockSSE2(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU没有SSE2")
	}
	if !guardChild(t) {
		return
	}
	for _, w := range []int{4, 8, 12, 16} {
		for _, unit := range []int{1, 2} {
			for _, both := range []bool{false, true} {
				t.Run(fmt.Sprintf("w%d/u%d/bi%t", w, unit, both), func(t *testing.T) {
					a, ss := guardedRows(t, w, 5, unit)
					b, _ := guardedRows(t, w, 5, unit)
					dst, ds := guardedRows(t, w, 5, unit)
					weights, offsets := [2]int32{-128, 127}, [2]int32{-128, 127}
					if !both {
						weights[1], offsets[1] = 0, 0
					}
					depth := 8
					if unit == 2 {
						depth = 10
					}
					p, _ := prepareBlockWeight(weights, offsets, 3, depth, both, unit == 2)
					if unit == 1 {
						for y := range 5 {
							for x := range w {
								a[y*ss+x], b[y*ss+x] = byte(x*71+y), byte(255-x*7-y)
							}
						}
						second := b
						if !both {
							second = nil
						}
						weightBlock8SSE2(dst, ds, a, second, ss, w, 5, p.coeff, p.bias, p.offset, p.shift)
						for y := range 5 {
							for x := range w {
								if dst[y*ds+x] != byte(weightBlockAnalytic(int64(a[y*ss+x]), int64(b[y*ss+x]), weights, offsets, 3, depth, both)) {
									t.Fatal("8-bit解析解不符")
								}
							}
						}
					} else {
						u, v, out := guardUint16(a), guardUint16(b), guardUint16(dst)
						for y := range 5 {
							for x := range w {
								u[y*ss+x], v[y*ss+x] = uint16(x*4131+y), uint16(65535-x*73-y)
							}
						}
						second := v
						if !both {
							second = nil
						}
						weightBlock16SSE2(out, ds, u, second, ss, w, 5, p.coeff, p.bias, p.offset, p.shift, 1023)
						for y := range 5 {
							for x := range w {
								if out[y*ds+x] != uint16(weightBlockAnalytic(int64(u[y*ss+x]), int64(v[y*ss+x]), weights, offsets, 3, depth, both)) {
									t.Fatal("uint16解析解不符")
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestWeightBlockSSE2_Admission(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU没有SSE2")
	}
	var a, b, d [256]byte
	var u, v, out [256]uint16
	weights, offsets := [2]int32{-64, 127}, [2]int32{-128, 127}
	if !TryWeightBlock8(d[:], 16, a[:], 16, 16, 16, -32, 127, 5) ||
		!TryBiWeightBlock8(d[:], 16, a[:], b[:], 16, 16, 16, weights, offsets, 6) ||
		!TryWeightBlock16(out[:], 16, u[:], 16, 16, 16, -32, 127, 5, 10) ||
		!TryBiWeightBlock16(out[:], 16, u[:], v[:], 16, 16, 16, weights, offsets, 6, 10) {
		t.Fatal("真实合格块没有进入SSE2")
	}
	if TryWeightBlock8(d[:], 16, a[:], 16, 7, 5, 1, 0, 0) ||
		TryWeightBlock8(d[:], 16, a[:], 16, 16, 16, 1<<30, 0, 0) ||
		TryWeightBlock16(out[:], 16, u[:], 16, 16, 16, 1, 0, 0, 9) ||
		TryWeightBlock8(d[1:], 16, d[:], 16, 16, 15, 1, 0, 0) {
		t.Fatal("不满足形状/参数/别名条件的块错误进入SSE2")
	}
}
