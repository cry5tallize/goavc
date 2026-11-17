//go:build amd64 && !purego

package simd

import (
	"fmt"
	"slices"
	"testing"
)

func TestWeightBlockAVX2_AnalyticAndBounds(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("当前CPU没有AVX2")
	}
	for _, depth := range []int{8, 10} {
		for _, both := range []bool{false, true} {
			for denom := uint32(0); denom <= 7; denom++ {
				for _, weight := range []int32{-128, -1, 0, 1, 127, 128} {
					for _, offset := range []int32{-128, -1, 0, 127} {
						weights, offsets := [2]int32{weight, 127}, [2]int32{offset, -128}
						if !both {
							weights[1], offsets[1] = 0, 0
						}
						p, _ := prepareBlockWeight(weights, offsets, denom, depth, both, depth == 10)
						var a, b [63]byte
						var u, v [63]uint16
						var got, want [63]byte
						var high, expected [63]uint16
						for i := range a {
							a[i], b[i] = byte(i*71), byte(i*37+19)
							u[i], v[i] = uint16(i*3211), uint16(65535-i*131)
							got[i], want[i], high[i], expected[i] = 77, 77, 777, 777
						}
						u[1], u[2], u[3], u[4] = 0, 1023, 32768, 65535
						originalA, originalB, originalU, originalV := a, b, u, v
						if depth == 8 {
							second := b[1:]
							if !both {
								second = nil
							}
							weightBlock8AVX2(got[1:], 23, a[1:], second, 21, 16, 3, p.coeff, p.bias, p.offset, p.shift)
						} else {
							second := v[1:]
							if !both {
								second = nil
							}
							weightBlock16AVX2(high[1:], 23, u[1:], second, 21, 16, 3, p.coeff, p.bias, p.offset, p.shift, 1023)
						}
						for y := range 3 {
							for x := range 16 {
								i, j := 1+y*23+x, 1+y*21+x
								want[i] = byte(weightBlockAnalytic(int64(a[j]), int64(b[j]), weights, offsets, denom, depth, both))
								expected[i] = uint16(weightBlockAnalytic(int64(u[j]), int64(v[j]), weights, offsets, denom, depth, both))
							}
						}
						if depth == 8 && got != want || depth == 10 && high != expected || a != originalA || b != originalB || u != originalU || v != originalV {
							t.Fatalf("depth%d bi%t denom%d w%d off%d解析值或未写区不同", depth, both, denom, weight, offset)
						}
					}
				}
			}
		}
	}
}

func TestGuardPage_WeightBlockAVX2(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("当前CPU没有AVX2")
	}
	if !guardChild(t) {
		return
	}
	for _, unit := range []int{1, 2} {
		for _, both := range []bool{false, true} {
			a, ss := guardedRows(t, 16, 5, unit)
			b, _ := guardedRows(t, 16, 5, unit)
			d, ds := guardedRows(t, 16, 5, unit)
			weights := [2]int32{-128, 127}
			if !both {
				weights[1] = 0
			}
			p, _ := prepareBlockWeight(weights, [2]int32{}, 3, 10, both, unit == 2)
			if unit == 1 {
				second := b
				if !both {
					second = nil
				}
				weightBlock8AVX2(d, ds, a, second, ss, 16, 5, p.coeff, p.bias, p.offset, p.shift)
			} else {
				u, v, out := guardUint16(a), guardUint16(b), guardUint16(d)
				second := v
				if !both {
					second = nil
				}
				weightBlock16AVX2(out, ds, u, second, ss, 16, 5, p.coeff, p.bias, p.offset, p.shift, 1023)
			}
		}
	}
}

func BenchmarkWeightWide_16_Backend(b *testing.B) {
	previous := kernels
	defer func() { kernels = previous }()
	for _, backend := range []Backend{SSE2, AVX2} {
		if backend == AVX2 && !nativeCPU.avx2 || !nativeCPU.sse2 {
			continue
		}
		for _, depth := range []int{8, 10} {
			for _, both := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/depth%d/bi%t", backend, depth, both), func(b *testing.B) {
					kernels.backends.Weight8, kernels.backends.Weight16 = backend, backend
					var a, c, d [368]byte
					var u, v, out [368]uint16
					for i := range a {
						a[i], c[i], u[i], v[i] = byte(i*71), byte(i*37), uint16(i*31%1024), uint16(i*73%1024)
					}
					b.ReportAllocs()
					for b.Loop() {
						if depth == 8 {
							if both {
								TryBiWeightBlock8(d[:], 23, a[:], c[:], 23, 16, 16, [2]int32{37, 27}, [2]int32{-7, 9}, 5)
							} else {
								TryWeightBlock8(d[:], 23, a[:], 23, 16, 16, 37, -7, 5)
							}
						} else {
							if both {
								TryBiWeightBlock16(out[:], 23, u[:], v[:], 23, 16, 16, [2]int32{37, 27}, [2]int32{-7, 9}, 5, 10)
							} else {
								TryWeightBlock16(out[:], 23, u[:], 23, 16, 16, 37, -7, 5, 10)
							}
						}
					}
				})
			}
		}
	}
}

func TestWeightBlockAVX2_DispatchInPlaceAndAllocation(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("当前CPU没有AVX2")
	}
	previous := kernels
	defer func() { kernels = previous }()
	kernels = selectKernels(cpuFeatures{sse2: true, avx2: true})
	if kernels.backends.Weight8 != AVX2 || kernels.backends.Weight16 != AVX2 {
		t.Fatal("AVX2槽未安装")
	}
	for _, w := range []int{4, 8, 12, 16} {
		var a, b [23 * 7]uint16
		for i := range a {
			a[i], b[i] = uint16(i*3911), uint16(i*7319)
		}
		want := a
		for y := range 7 {
			for x := range w {
				i := y*23 + x
				want[i] = uint16(weightBlockAnalytic(int64(a[i]), int64(b[i]), [2]int32{37, 27}, [2]int32{-7, 9}, 5, 10, true))
			}
		}
		if !TryBiWeightBlock16(a[:], 23, a[:], b[:], 23, w, 7, [2]int32{37, 27}, [2]int32{-7, 9}, 5, 10) || !slices.Equal(a[:], want[:]) {
			t.Fatalf("宽%d原地分派不符", w)
		}
	}
	if n := testing.AllocsPerRun(100, func() {
		var a, b, d [256]byte
		TryBiWeightBlock8(d[:], 16, a[:], b[:], 16, 16, 16, [2]int32{37, 27}, [2]int32{-7, 9}, 5)
	}); n != 0 {
		t.Fatalf("局部数组分配=%g", n)
	}
}

func TestWeightBlockAVX2_WordStorageEightBitOutput(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("当前CPU没有AVX2")
	}
	for _, both := range []bool{false, true} {
		for denom := uint32(0); denom <= 7; denom++ {
			weights, offsets := [2]int32{-128, 127}, [2]int32{-128, 127}
			if !both {
				weights[1], offsets[1] = 0, 0
			}
			p, _ := prepareBlockWeight(weights, offsets, denom, 8, both, true)
			var a, c, d [32]uint16
			for i := range a {
				a[i], c[i] = uint16(i*3911), uint16(65535-i*131)
			}
			second := c[:]
			if !both {
				second = nil
			}
			weightBlock16AVX2(d[:], 16, a[:], second, 16, 16, 2, p.coeff, p.bias, p.offset, p.shift, 255)
			for i := range d {
				if d[i] != uint16(weightBlockAnalytic(int64(a[i]), int64(c[i]), weights, offsets, denom, 8, both)) {
					t.Fatal("uint16存储/8-bit裁剪不符")
				}
			}
		}
	}
}

func BenchmarkWeightHeight_16_Backend(b *testing.B) {
	previous := kernels
	defer func() { kernels = previous }()
	if !nativeCPU.avx2 {
		b.Skip("当前CPU没有AVX2")
	}
	for _, backend := range []Backend{SSE2, AVX2} {
		for _, h := range []int{1, 2, 4, 8, 16} {
			b.Run(fmt.Sprintf("%s/h%d", backend, h), func(b *testing.B) {
				kernels.backends.Weight8 = backend
				var a, c, d [368]byte
				b.ReportAllocs()
				for b.Loop() {
					TryBiWeightBlock8(d[:], 23, a[:], c[:], 23, 16, h, [2]int32{37, 27}, [2]int32{-7, 9}, 5)
				}
			})
		}
	}
}

// 准入门禁中的小高度、单行与窄宽控制组，防止16×16胜出掩盖包装回退。
func BenchmarkWeightShape_Block_Backend(b *testing.B) {
	previous := kernels
	defer func() { kernels = previous }()
	if !nativeCPU.avx2 {
		b.Skip("当前CPU没有AVX2")
	}
	for _, backend := range []Backend{SSE2, AVX2} {
		for _, shape := range [][2]int{{16, 1}, {16, 2}, {16, 4}, {8, 8}, {4, 4}, {12, 7}} {
			for _, depth := range []int{8, 10} {
				for _, both := range []bool{false, true} {
					b.Run(fmt.Sprintf("%s/w%dh%d/depth%d/bi%t", backend, shape[0], shape[1], depth, both), func(b *testing.B) {
						kernels.backends.Weight8, kernels.backends.Weight16 = backend, backend
						var a, c, d [368]byte
						var u, v, out [368]uint16
						for i := range a {
							a[i], c[i], u[i], v[i] = byte(i*71), byte(i*37), uint16(i*31%1024), uint16(i*73%1024)
						}
						b.ReportAllocs()
						for b.Loop() {
							if depth == 8 {
								if both {
									TryBiWeightBlock8(d[:], 23, a[:], c[:], 23, shape[0], shape[1], [2]int32{37, 27}, [2]int32{-7, 9}, 5)
								} else {
									TryWeightBlock8(d[:], 23, a[:], 23, shape[0], shape[1], 37, -7, 5)
								}
							} else {
								if both {
									TryBiWeightBlock16(out[:], 23, u[:], v[:], 23, shape[0], shape[1], [2]int32{37, 27}, [2]int32{-7, 9}, 5, 10)
								} else {
									TryWeightBlock16(out[:], 23, u[:], 23, shape[0], shape[1], 37, -7, 5, 10)
								}
							}
						}
					})
				}
			}
		}
	}
}
