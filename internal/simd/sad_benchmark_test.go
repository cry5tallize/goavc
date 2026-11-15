package simd

import (
	"fmt"
	"testing"
)

var sadBenchmarkResult int

func benchmarkSAD8(b *testing.B, fn sad8Func) {
	var a, ref [256]byte
	for i := range a {
		a[i], ref[i] = byte(i), byte(i*7)
	}
	if got, want := fn(a[:], 16, ref[:], 16, 16, 16), sadGeneric(a[:], 16, ref[:], 16, 16, 16); got != want {
		b.Fatalf("SAD8=%d，期望%d", got, want)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBenchmarkResult = fn(a[:], 16, ref[:], 16, 16, 16)
	}
}

func benchmarkSAD16(b *testing.B, fn sad16Func) {
	var a, ref [256]uint16
	for i := range a {
		a[i], ref[i] = uint16(i*3), uint16(i*7%1024)
	}
	if got, want := fn(a[:], 16, ref[:], 16, 16, 16), sadGeneric(a[:], 16, ref[:], 16, 16, 16); got != want {
		b.Fatalf("SAD16=%d，期望%d", got, want)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBenchmarkResult = fn(a[:], 16, ref[:], 16, 16, 16)
	}
}

func BenchmarkSAD8_16x16_Generic(b *testing.B)  { benchmarkSAD8(b, sadGeneric[byte]) }
func BenchmarkSAD16_16x16_Generic(b *testing.B) { benchmarkSAD16(b, sadGeneric[uint16]) }
func BenchmarkSAD8_16x16_Auto(b *testing.B)     { benchmarkSAD8(b, SAD8) }
func BenchmarkSAD16_16x16_Auto(b *testing.B)    { benchmarkSAD16(b, SAD16) }

// 16×16的Generic/Auto沿用历史入口；其余分区和非整向量尾部共用真实包装。
func BenchmarkSADBlocks(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		for _, size := range [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}, {7, 13}} {
			if size == [2]int{16, 16} && (backend.name == "Generic" || backend.name == "Auto") {
				continue
			}
			w, h := size[0], size[1]
			b.Run(fmt.Sprintf("%dx%d/%s", w, h, backend.name), func(b *testing.B) {
				kernels = backend.table
				b.Run("SAD8", func(b *testing.B) {
					var a, ref [19 * 16]byte
					for i := range a {
						a[i], ref[i] = byte(i*31), byte(i*7+19)
					}
					want := sadGeneric(a[:], 19, ref[:], 19, w, h)
					if got := SAD8(a[:], 19, ref[:], 19, w, h); got != want {
						b.Fatalf("SAD8=%d，期望%d", got, want)
					}
					b.ReportAllocs()
					for b.Loop() {
						sadBenchmarkResult = SAD8(a[:], 19, ref[:], 19, w, h)
					}
				})
				b.Run("SAD16", func(b *testing.B) {
					var a, ref [19 * 16]uint16
					for i := range a {
						a[i], ref[i] = uint16(i*31%1024), uint16((i*7+19)%1024)
					}
					want := sadGeneric(a[:], 19, ref[:], 19, w, h)
					if got := SAD16(a[:], 19, ref[:], 19, w, h); got != want {
						b.Fatalf("SAD16=%d，期望%d", got, want)
					}
					b.ReportAllocs()
					for b.Loop() {
						sadBenchmarkResult = SAD16(a[:], 19, ref[:], 19, w, h)
					}
				})
			})
		}
	}
}
