package simd

import (
	"fmt"
	"testing"
)

func BenchmarkFilter_4Lines(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		b.Run(backend.name, func(b *testing.B) {
			kernels = backend.table
			for _, strong := range []bool{false, true} {
				b.Run(fmt.Sprint("strong", strong), func(b *testing.B) {
					var initial [32]byte
					for i := range initial {
						initial[i] = byte(100 + i%8*2)
					}
					bs := 2
					if strong {
						bs = 4
					}
					b.ReportAllocs()
					for b.Loop() {
						data := initial
						FilterLuma8(data[:], 4, 8, 1, bs, 40, 20, 2, 4, 8)
						filterBenchSink = data
					}
				})
			}
		})
	}
}

var filterBenchSink [32]byte
var residualBenchSink [64]int32

func BenchmarkResidual_8x8(b *testing.B) {
	var initial, pred [64]uint16
	var r [64]int32
	for i := range initial {
		initial[i] = uint16(512 + i%4)
		pred[i] = uint16(515 - i%4)
		r[i] = int32(i*13 - 300)
	}
	b.Run("ScalarBeforeAdd16", func(b *testing.B) {
		for b.Loop() {
			d := initial
			for i, v := range r {
				sum := int64(d[i]) + int64(v)
				d[i] = uint16(max(0, min(int64(1023), sum)))
			}
			residualHighSink = d
		}
	})
	b.Run("ScalarBeforeSub16", func(b *testing.B) {
		for b.Loop() {
			for i, v := range initial {
				residualBenchSink[i] = int32(v) - int32(pred[i])
			}
		}
	})
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		b.Run(backend.name, func(b *testing.B) {
			kernels = backend.table
			b.Run("Add16", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					d := initial
					AddResidual16(d[:], 8, r[:], 8, 8, 8)
					residualHighSink = d
				}
			})
			b.Run("Sub16", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					Subtract16(residualBenchSink[:], 8, initial[:], 8, pred[:], 8, 8, 8)
				}
			})
		})
	}
}

var residualHighSink [64]uint16

func BenchmarkDequantization_8x8(b *testing.B) {
	var src [64]int32
	var mul [64]int64
	for i := range src {
		src[i] = int32(i*3 - 80)
		mul[i] = 320 << 4
	}
	b.Run("ScalarBefore", func(b *testing.B) {
		for b.Loop() {
			for i, v := range src {
				v := (int64(v)*mul[i] + 32) >> 6
				if v < -1<<31 || v > 1<<31-1 {
					b.Fatal("反量化意外溢出")
				}
				quantWideBenchSink[i] = int32(v)
			}
		}
	})
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		b.Run(backend.name, func(b *testing.B) {
			kernels = backend.table
			b.ReportAllocs()
			for b.Loop() {
				n := Dequant8x8(&quantWideBenchSink, &src, &mul)
				if n != 64 {
					b.Fatal("反量化意外溢出")
				}
			}
		})
	}
}

var quantWideBenchSink [64]int32
