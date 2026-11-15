package simd

import "testing"

var transformChromaBenchmarkSink [4]int32

func BenchmarkForward8x8_Generic(b *testing.B) {
	var src [64]int32
	for i := range src {
		src[i] = int32(i*31 - 960)
	}
	b.ReportAllocs()
	for range b.N {
		_ = forward8Generic(src)
	}
}

func BenchmarkIDCT4x4_Generic(b *testing.B) {
	var src [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	for range b.N {
		_ = idct4Generic(src)
	}
}

func BenchmarkIDCT8x8_Generic(b *testing.B) {
	var src [64]int32
	for i := range src {
		src[i] = int32(i*31 - 960)
	}
	b.ReportAllocs()
	for range b.N {
		_, _ = idct8Generic(src)
	}
}

func BenchmarkForwardLumaDC_Generic(b *testing.B) {
	var src [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	for range b.N {
		_ = forwardLumaDCGeneric(src)
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
		_ = forwardChromaDC422Generic(src)
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
