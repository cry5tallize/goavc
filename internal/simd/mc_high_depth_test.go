package simd

import (
	"fmt"
	"slices"
	"testing"
)

func TestMCHighDepth_AllPhasesAndBounds(t *testing.T) {
	previous := kernels
	defer func() { kernels = previous }()
	for _, w := range []int{1, 3, 4, 8, 12, 16} {
		for _, h := range []int{1, 7, 16} {
			for fy := range 4 {
				for fx := range 4 {
					rw, rh, rs, ds := w, h, 29, 23
					if fx != 0 {
						rw += 5
					}
					if fy != 0 {
						rh += 5
					}
					for _, pattern := range []int{0, 1, 2, 3} {
						src := make([]uint16, 1+(rh-1)*rs+rw)
						for i := range src {
							switch pattern {
							case 0:
								src[i] = uint16((i*71 + i/17*13) % 1024)
							case 1:
								src[i] = uint16(i % 2 * 1023)
							case 2:
								src[i] = 1023
							case 3:
								// 保留内部uint16旧入口的宽域行为，不假定外部样本校验替代算术精度。
								src[i] = uint16(i * 3911)
							}
						}
						original := slices.Clone(src)
						got := make([]uint16, 1+(h-1)*ds+w)
						for i := range got {
							got[i] = 777
						}
						want := slices.Clone(got)
						kernels = previous
						kernels.backends.MCLumaFractional16 = Generic
						MCLuma(want[1:], ds, src[1:], rs, w, h, fx, fy, 10)
						kernels = previous
						MCLuma(got[1:], ds, src[1:], rs, w, h, fx, fy, 10)
						if !slices.Equal(got, want) || !slices.Equal(src, original) {
							t.Fatalf("%dx%d phase%d,%d pattern%d结果/未写区/源改变", w, h, fx, fy, pattern)
						}
					}
				}
			}
		}
	}
}

func TestMCHighDepth_AnalyticRampAndSignedIntermediate(t *testing.T) {
	// 线性场的六抽头精确再现分数位置；斜率8/16使全部qpel均为整数。
	for fy := range 4 {
		for fx := range 4 {
			var src [21 * 29]uint16
			var dst [16 * 23]uint16
			for y := range 21 {
				for x := range 21 {
					src[y*29+x] = uint16(101 + 8*x + 16*y)
				}
			}
			MCLuma(dst[:], 23, src[:], 29, 16, 16, fx, fy, 10)
			left, top := 0, 0
			if fx != 0 {
				left = 2
			}
			if fy != 0 {
				top = 2
			}
			for y := range 16 {
				for x := range 16 {
					want := uint16(101 + 8*(left+x) + 16*(top+y) + 2*fx + 4*fy)
					if dst[y*23+x] != want {
						t.Fatalf("phase%d,%d at%d,%d=%d want%d", fx, fy, x, y, dst[y*23+x], want)
					}
				}
			}
		}
	}
	for _, tc := range []struct {
		profile [6]int
		want    uint16
	}{
		{[6]int{0, 1, 0, 0, 1, 0}, 100},
		{[6]int{0, 0, 1, 1, 0, 0}, 1023},
	} {
		var src [6 * 9]uint16
		var dst [4]uint16
		for y := range 6 {
			for x := range 6 {
				src[y*9+x] = uint16(1023 * tc.profile[x] * tc.profile[y])
			}
		}
		// 第一例水平-10230，二维+102300，若提前Clip会错误变为0。
		MCLuma(dst[:], 4, src[:], 9, 4, 1, 2, 2, 10)
		if dst[0] != tc.want {
			t.Fatalf("signed intermediate=%d want%d", dst[0], tc.want)
		}
	}
}

func BenchmarkMCHighDepth_Block_Backend(b *testing.B) {
	previous := kernels
	defer func() { kernels = previous }()
	for _, backend := range []Backend{Generic, previous.backends.MCLumaFractional16} {
		if backend == Generic && previous.backends.MCLumaFractional16 == Generic {
			continue
		}
		for _, w := range []int{4, 8, 12, 16} {
			for _, phase := range [][2]int{{2, 0}, {0, 2}, {2, 2}, {1, 3}, {3, 2}} {
				b.Run(fmt.Sprintf("%s/w%d/phase%d%d", backend, w, phase[0], phase[1]), func(b *testing.B) {
					kernels.backends.MCLumaFractional16 = backend
					var src [21 * 29]uint16
					var dst [16 * 23]uint16
					for i := range src {
						src[i] = uint16(i * 71 % 1024)
					}
					b.ReportAllocs()
					for b.Loop() {
						MCLuma(dst[:], 23, src[:], 29, w, 16, phase[0], phase[1], 10)
					}
				})
			}
		}
	}
}

func TestMCHighDepth_LocalZeroAlloc(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		var src [21 * 29]uint16
		var dst [16 * 23]uint16
		MCLuma(dst[:], 23, src[:], 29, 16, 16, 2, 2, 10)
		MCChroma(dst[:], 23, src[:], 29, 16, 16, 3, 7)
	}); n != 0 {
		t.Fatalf("局部MC分配=%g", n)
	}
}

func TestMCChromaHighDepth_AllPhases(t *testing.T) {
	previous := kernels
	defer func() { kernels = previous }()
	for _, w := range []int{1, 3, 4, 8, 12, 16} {
		for fy := range 8 {
			for fx := range 8 {
				rw, rh := w, 7
				if fx != 0 {
					rw++
				}
				if fy != 0 {
					rh++
				}
				var src [8 * 29]uint16
				for i := range src {
					src[i] = uint16(i * 3911)
				}
				var got, want [7 * 23]uint16
				for i := range got {
					got[i], want[i] = 777, 777
				}
				kernels.backends.MCChroma16 = Generic
				MCChroma(want[1:1+6*23+w], 23, src[1:1+(rh-1)*29+rw], 29, w, 7, fx, fy)
				kernels = previous
				MCChroma(got[1:1+6*23+w], 23, src[1:1+(rh-1)*29+rw], 29, w, 7, fx, fy)
				if got != want {
					t.Fatalf("chroma w%d phase%d,%d不符", w, fx, fy)
				}
			}
		}
	}
}

func BenchmarkMCChromaHighDepth_Block_Backend(b *testing.B) {
	previous := kernels
	defer func() { kernels = previous }()
	for _, backend := range []Backend{Generic, previous.backends.MCChroma16} {
		if previous.backends.MCChroma16 == Generic {
			continue
		}
		for _, w := range []int{4, 8, 12, 16} {
			for _, phase := range [][2]int{{3, 0}, {0, 7}, {3, 7}} {
				b.Run(fmt.Sprintf("%s/w%d/phase%d%d", backend, w, phase[0], phase[1]), func(b *testing.B) {
					kernels.backends.MCChroma16 = backend
					var src [17 * 29]uint16
					var dst [16 * 23]uint16
					for i := range src {
						src[i] = uint16(i * 71 % 1024)
					}
					b.ReportAllocs()
					for b.Loop() {
						MCChroma(dst[:], 23, src[:], 29, w, 16, phase[0], phase[1])
					}
				})
			}
		}
	}
}
