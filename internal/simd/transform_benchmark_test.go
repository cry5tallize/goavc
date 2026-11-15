package simd

import (
	"fmt"
	"testing"
)

var transformChromaBenchmarkSink [4]int32

func BenchmarkForward8x8_Generic(b *testing.B) {
	var src [64]int32
	for i := range src {
		src[i] = int32(i*31 - 960)
	}
	b.ReportAllocs()
	for range b.N {
		transformLargeBenchmarkSink = forward8Generic(src)
	}
}

func BenchmarkIDCT4x4_Generic(b *testing.B) {
	var src [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	for range b.N {
		transformSmallBenchmarkSink = idct4Generic(src)
	}
}

func BenchmarkIDCT8x8_Generic(b *testing.B) {
	var src [64]int32
	for i := range src {
		src[i] = int32(i*31 - 960)
	}
	b.ReportAllocs()
	for range b.N {
		transformLargeBenchmarkSink, transformWrittenBenchmarkSink = idct8Generic(src)
	}
}

func BenchmarkForwardLumaDC_Generic(b *testing.B) {
	var src [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	for range b.N {
		transformSmallBenchmarkSink = forwardLumaDCGeneric(src)
	}
}

func BenchmarkForwardChromaDC_Generic(b *testing.B) {
	src := [4]int32{1023, -1023, 511, -511}
	b.ReportAllocs()
	for range b.N {
		transformChromaBenchmarkSink = forwardChromaDCGeneric(src)
	}
}

func BenchmarkForwardChromaDC422_Generic(b *testing.B) {
	src := [8]int32{1023, -1023, 511, -511, 1, -1, 255, -255}
	b.ReportAllocs()
	for range b.N {
		transformDC422BenchmarkSink = forwardChromaDC422Generic(src)
	}
}

// 真实块分派入口使用同一输入和可观察输出，避免纯Go结果被死代码消除。
func BenchmarkTransformBlocks(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	var s4 [16]int32
	var s8 [64]int32
	for i := range s4 {
		s4[i] = int32(i*31 - 237)
	}
	for i := range s8 {
		s8[i] = int32(i*31 - 960)
	}
	for _, backend := range guardBackends() {
		b.Run(backend.name, func(b *testing.B) {
			kernels = backend.table
			if got := Forward4x4(s4); got != forward4Generic(s4) {
				b.Fatal("Forward4预校验失败")
			}
			var out8 [64]int32
			Forward8x8(&out8, &s8)
			if out8 != forward8Generic(s8) {
				b.Fatal("Forward8预校验失败")
			}
			var out4 [16]int32
			IDCT4x4(&out4, &s4)
			if out4 != idct4Generic(s4) {
				b.Fatal("IDCT4预校验失败")
			}
			want8, written := idct8Generic(s8)
			if ok := IDCT8x8(&out8, &s8); !ok || written != 64 || out8 != want8 {
				b.Fatal("IDCT8预校验失败")
			}
			if ForwardLumaDC(s4) != forwardLumaDCGeneric(s4) ||
				ForwardChromaDC([4]int32{1023, -1023, 511, -511}) != forwardChromaDCGeneric([4]int32{1023, -1023, 511, -511}) ||
				ForwardChromaDC422([8]int32{1, -1, 2, -2, 3, -3, 4, -4}) != forwardChromaDC422Generic([8]int32{1, -1, 2, -2, 3, -3, 4, -4}) {
				b.Fatal("DC预校验失败")
			}
			b.Run("Forward4", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					transformSmallBenchmarkSink = Forward4x4(s4)
				}
			})
			b.Run("Forward8", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					Forward8x8(&transformLargeBenchmarkSink, &s8)
				}
			})
			b.Run("IDCT4", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					IDCT4x4(&transformSmallBenchmarkSink, &s4)
				}
			})
			b.Run("IDCT8", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if !IDCT8x8(&transformLargeBenchmarkSink, &s8) {
						b.Fatal("合法系数逆变换溢出")
					}
				}
			})
			b.Run("LumaDC", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					transformSmallBenchmarkSink = ForwardLumaDC(s4)
				}
			})
			b.Run("ChromaDC", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					transformChromaBenchmarkSink = ForwardChromaDC([4]int32{1023, -1023, 511, -511})
				}
			})
			b.Run("ChromaDC422", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					transformDC422BenchmarkSink = ForwardChromaDC422([8]int32{1, -1, 2, -2, 3, -3, 4, -4})
				}
			})
		})
	}
}

var transformDC422BenchmarkSink [8]int64
var transformSmallBenchmarkSink [16]int32
var transformLargeBenchmarkSink [64]int32

var transformWrittenBenchmarkSink int
var transformDCPixelBenchmarkSink int32

// 宽域与普通残差分开；Forward8的第二遍含真实int64乘积回退，名称明确标出。
func BenchmarkTransformWide(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	var s4 [16]int32
	var s8 [64]int32
	for i := range s4 {
		s4[i] = int32((i%3 - 1) * (1 << 29))
	}
	for i := range s8 {
		s8[i] = 1 << 24
	}
	for _, backend := range guardBackends() {
		b.Run(backend.name, func(b *testing.B) {
			kernels = backend.table
			b.Run("Forward4", func(b *testing.B) {
				if Forward4x4(s4) != forward4Generic(s4) {
					b.Fatal("宽域Forward4预校验失败")
				}
				b.ReportAllocs()
				for b.Loop() {
					transformSmallBenchmarkSink = Forward4x4(s4)
				}
			})
			b.Run("Forward8-Int64Fallback", func(b *testing.B) {
				var got [64]int32
				Forward8x8(&got, &s8)
				if got != forward8Generic(s8) {
					b.Fatal("宽域Forward8预校验失败")
				}
				b.ReportAllocs()
				for b.Loop() {
					Forward8x8(&transformLargeBenchmarkSink, &s8)
				}
			})
			b.Run("IDCT4", func(b *testing.B) {
				var got [16]int32
				IDCT4x4(&got, &s4)
				if got != idct4Generic(s4) {
					b.Fatal("宽域IDCT4预校验失败")
				}
				b.ReportAllocs()
				for b.Loop() {
					IDCT4x4(&transformSmallBenchmarkSink, &s4)
				}
			})
			b.Run("IDCT8", func(b *testing.B) {
				var got [64]int32
				want, written := idct8Generic(s8)
				if ok := IDCT8x8(&got, &s8); !ok || written != 64 || got != want {
					b.Fatal("宽域IDCT8预校验失败")
				}
				b.ReportAllocs()
				for b.Loop() {
					transformOKBenchmarkSink = IDCT8x8(&transformLargeBenchmarkSink, &s8)
				}
			})
		})
	}
}

var transformOKBenchmarkSink bool

func BenchmarkIDCT4DC(b *testing.B) {
	// 此入口固定为标量，与分派IDCT4整块基准分开，不能标成ISA内核。
	for _, dc := range []int32{-1 << 31, -65537, 0, 65537, 1<<31 - 1} {
		b.Run(fmt.Sprintf("%d/Generic", dc), func(b *testing.B) {
			want := int32((int64(dc) + 32) >> 6)
			if got := IDCT4x4DC(dc); got != want {
				b.Fatalf("DC逆变换=%d，期望%d", got, want)
			}
			b.ReportAllocs()
			for b.Loop() {
				transformDCPixelBenchmarkSink = IDCT4x4DC(dc)
			}
		})
	}
}

var transformDC422InverseBenchmarkSink [8]int32

func BenchmarkDCInverse(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		name := benchmarkBackendName(b, backend, backend.table.backends.WideAdd4)
		if name == "" {
			continue
		}
		for _, wide := range []bool{false, true} {
			domain := "Normal"
			if wide {
				domain = "Wide-Int64Fallback"
			}
			b.Run(domain+"/"+name, func(b *testing.B) {
				var luma [16]int32
				var chroma [4]int32
				var chroma422 [8]int32
				mul := int64(13312)
				for i := range luma {
					luma[i] = int32(i*61 - 451)
					if wide {
						luma[i] = 1 << 29
					}
				}
				for i := range chroma {
					chroma[i] = int32(i*61 - 91)
					if wide {
						chroma[i] = 1 << 30
					}
				}
				for i := range chroma422 {
					chroma422[i] = int32(i*61 - 211)
					if wide {
						chroma422[i] = 1 << 29
					}
				}
				if wide {
					mul = 1
				}
				kernels = genericTable()
				wantLuma, nl := DequantLumaDC(luma, mul)
				wantChroma, nc := DequantChromaDC(chroma, mul)
				want422, n422 := DequantChromaDC422(chroma422, mul)
				if nl != 16 || nc != 4 || n422 != 8 {
					b.Fatal("DC基准夹具溢出")
				}
				kernels = backend.table
				b.Run("Luma", func(b *testing.B) {
					if got, n := DequantLumaDC(luma, mul); got != wantLuma || n != nl {
						b.Fatal("逆亮度DC预校验失败")
					}
					b.ReportAllocs()
					for b.Loop() {
						transformSmallBenchmarkSink, transformWrittenBenchmarkSink = DequantLumaDC(luma, mul)
					}
				})
				b.Run("Chroma420", func(b *testing.B) {
					if got, n := DequantChromaDC(chroma, mul); got != wantChroma || n != nc {
						b.Fatal("逆420色度DC预校验失败")
					}
					b.ReportAllocs()
					for b.Loop() {
						transformChromaBenchmarkSink, transformWrittenBenchmarkSink = DequantChromaDC(chroma, mul)
					}
				})
				b.Run("Chroma422", func(b *testing.B) {
					if got, n := DequantChromaDC422(chroma422, mul); got != want422 || n != n422 {
						b.Fatal("逆422色度DC预校验失败")
					}
					b.ReportAllocs()
					for b.Loop() {
						transformDC422InverseBenchmarkSink, transformWrittenBenchmarkSink = DequantChromaDC422(chroma422, mul)
					}
				})
			})
		}
	}
}
