package simd

import (
	"fmt"
	"testing"
)

// Auto显式附带实际槽位；未实现的指定ISA不能借Generic同名出场。
func benchmarkBackendName(b *testing.B, backend guardBackend, actual Backend) string {
	b.Helper()
	if backend.name == "Auto" {
		return "Auto-" + actual.String()
	}
	if backend.name != actual.String() {
		return ""
	}
	return backend.name
}

var satdBenchmarkSink int

// 每op真正切出并处理全部4×4 tile，不用一次调用冒充批量SATD。
func BenchmarkSATDBlocks(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		name := benchmarkBackendName(b, backend, backend.table.backends.SATD4x4)
		if name == "" {
			continue
		}
		for _, depth := range []int{8, 10} {
			for _, size := range []int{4, 8, 16} {
				b.Run(fmt.Sprintf("%dx%d/%dbit/%s", size, size, depth, name), func(b *testing.B) {
					kernels = backend.table
					var src [16 * 16]int32
					limit := 1<<depth - 1
					for i := range src {
						src[i] = int32((i*179+37)%(2*limit+1) - limit)
					}
					want, got := 0, 0
					for y := 0; y < size; y += 4 {
						for x := 0; x < size; x += 4 {
							var tile [16]int32
							for row := range 4 {
								copy(tile[row*4:row*4+4], src[(y+row)*16+x:][:4])
							}
							want += satd4Generic(tile)
							got += SATD4x4(tile)
						}
					}
					if got != want {
						b.Fatalf("tiled SATD=%d，期望%d", got, want)
					}
					b.ReportAllocs()
					for b.Loop() {
						sum := 0
						for y := 0; y < size; y += 4 {
							for x := 0; x < size; x += 4 {
								var tile [16]int32
								for row := range 4 {
									copy(tile[row*4:row*4+4], src[(y+row)*16+x:][:4])
								}
								sum += SATD4x4(tile)
							}
						}
						satdBenchmarkSink = sum
					}
				})
			}
		}
	}
}

var mcCopyBenchmarkSink [256]byte

// 仅8位16宽整数复制有ISA实现；插值和uint16不以ISA名称冒充快路。
func BenchmarkMCLumaCopy_16x16(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		name := benchmarkBackendName(b, backend, backend.table.backends.MCLumaCopy)
		if name == "" {
			continue
		}
		b.Run(name, func(b *testing.B) {
			kernels = backend.table
			var ref [23 * 16]byte
			for i := range ref {
				ref[i] = byte(i*37 + 13)
			}
			MCLuma(mcCopyBenchmarkSink[:], 16, ref[:], 23, 16, 16, 0, 0, 8)
			for y := range 16 {
				for x := range 16 {
					if mcCopyBenchmarkSink[y*16+x] != ref[y*23+x] {
						b.Fatal("整数MC复制不一致")
					}
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				MCLuma(mcCopyBenchmarkSink[:], 16, ref[:], 23, 16, 16, 0, 0, 8)
			}
		})
	}
}

var residualByteBenchmarkSink [64]byte

func BenchmarkResidualBlocks(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		name := benchmarkBackendName(b, backend, backend.table.backends.Residual)
		if name == "" {
			continue
		}
		for _, depth := range []int{8, 10} {
			for _, size := range []int{4, 8} {
				for _, domain := range []string{"NoClip", "ClipExtremes"} {
					b.Run(fmt.Sprintf("%dx%d/%dbit/%s/%s", size, size, depth, domain, name), func(b *testing.B) {
						kernels = backend.table
						var src8, pred8 [64]byte
						var src16, pred16 [64]uint16
						var residual [64]int32
						limit := 1<<depth - 1
						for i := range residual {
							src16[i], pred16[i] = uint16(limit/2+i%7), uint16(limit/2-i%7)
							src8[i], pred8[i] = byte(src16[i]), byte(pred16[i])
							residual[i] = int32(i%13 - 6)
							if domain == "ClipExtremes" {
								if i%2 == 0 {
									residual[i] = -1 << 31
								} else {
									residual[i] = 1<<31 - 1
								}
								src16[i], pred16[i] = uint16((1-i%2)*limit), uint16(i%2*limit)
								src8[i], pred8[i] = byte(src16[i]), byte(pred16[i])
							}
						}
						b.Run("Add", func(b *testing.B) {
							got8, got16 := src8, src16
							want8, want16 := src8, src16
							if depth == 8 {
								AddResidual8(got8[:], size, residual[:], size, size, size)
								residualAddScalar(want8[:], size, residual[:], size, size, size, limit)
							} else {
								AddResidual16(got16[:], size, residual[:], size, size, size)
								residualAddScalar(want16[:], size, residual[:], size, size, size, limit)
							}
							if got8 != want8 || got16 != want16 {
								b.Fatal("残差重建预校验失败")
							}
							b.ReportAllocs()
							if depth == 8 {
								for b.Loop() {
									dst := src8
									AddResidual8(dst[:], size, residual[:], size, size, size)
									residualByteBenchmarkSink = dst
								}
							} else {
								for b.Loop() {
									dst := src16
									AddResidual16(dst[:], size, residual[:], size, size, size)
									residualHighSink = dst
								}
							}
						})
						b.Run("Subtract", func(b *testing.B) {
							var got, want [64]int32
							if depth == 8 {
								Subtract8(got[:], size, src8[:], size, pred8[:], size, size, size)
								residualSubScalar(want[:], size, src8[:], size, pred8[:], size, size, size)
							} else {
								Subtract16(got[:], size, src16[:], size, pred16[:], size, size, size)
								residualSubScalar(want[:], size, src16[:], size, pred16[:], size, size, size)
							}
							if got != want {
								b.Fatal("残差相减预校验失败")
							}
							b.ReportAllocs()
							if depth == 8 {
								for b.Loop() {
									Subtract8(residualBenchSink[:], size, src8[:], size, pred8[:], size, size, size)
								}
							} else {
								for b.Loop() {
									Subtract16(residualBenchSink[:], size, src16[:], size, pred16[:], size, size, size)
								}
							}
						})
					})
				}
			}
		}
	}
}

var deblockByteBenchmarkSink [64]byte
var deblockHighBenchmarkSink [64]uint16

func BenchmarkDeblockSpans(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		name := benchmarkBackendName(b, backend, backend.table.backends.FilterDelta)
		if name == "" {
			continue
		}
		for _, depth := range []int{8, 10} {
			for _, scenario := range []struct {
				name                     string
				strong, vertical, chroma bool
				span                     int
			}{
				{"LumaWeakVerticalSpan1", false, true, false, 1},
				{"LumaWeakHorizontalSpan4", false, false, false, 4},
				{"LumaStrongVerticalSpan4", true, true, false, 4},
				{"LumaStrongHorizontalSpan1", true, false, false, 1},
				{"ChromaWeakVerticalSpan4", false, true, true, 4},
				{"ChromaStrongHorizontalSpan1", true, false, true, 1},
			} {
				b.Run(fmt.Sprintf("%s/%dbit/%s", scenario.name, depth, name), func(b *testing.B) {
					kernels = backend.table
					q0, lineStep, acrossStep := 32, 1, 8
					if scenario.vertical {
						q0, lineStep, acrossStep = 4, 8, 1
					}
					scale := 1 << (depth - 8)
					var initial8 [64]byte
					var initial16 [64]uint16
					for i := range initial16 {
						across := i / 8
						if scenario.vertical {
							across = i % 8
						}
						v := (96 + across*2) * scale
						if across >= 4 {
							v += 2 * scale
						}
						initial8[i], initial16[i] = byte(v), uint16(v)
					}
					strength := 2
					if scenario.strong {
						strength = 4
					}
					alpha, beta, tc := 40*scale, 20*scale, 2*scale
					got8, want8 := initial8, initial8
					got16, want16 := initial16, initial16
					if depth == 8 {
						if scenario.chroma {
							FilterChroma8(got8[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
							filterChroma(want8[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth, filterDeltaGeneric)
						} else {
							FilterLuma8(got8[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
							filterLuma(want8[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth, filterDeltaGeneric)
						}
					} else if scenario.chroma {
						FilterChroma16(got16[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
						filterChroma(want16[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth, filterDeltaGeneric)
					} else {
						FilterLuma16(got16[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
						filterLuma(want16[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth, filterDeltaGeneric)
					}
					if got8 != want8 || got16 != want16 {
						b.Fatal("去块滤波预校验失败")
					}
					// 每op恢复相同边缘，固定数组复制计时；避免重复滤波收敛成空操作。
					b.ReportAllocs()
					if depth == 8 {
						for b.Loop() {
							dst := initial8
							if scenario.chroma {
								FilterChroma8(dst[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
							} else {
								FilterLuma8(dst[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
							}
							deblockByteBenchmarkSink = dst
						}
					} else {
						for b.Loop() {
							dst := initial16
							if scenario.chroma {
								FilterChroma16(dst[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
							} else {
								FilterLuma16(dst[:], q0, lineStep, acrossStep, strength, alpha, beta, tc, scenario.span, depth)
							}
							deblockHighBenchmarkSink = dst
						}
					}
				})
			}
		}
	}
}
