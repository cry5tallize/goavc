package simd

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestQuantization_ExactProductsAndFallback(t *testing.T) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		t.Run(backend.name, func(t *testing.T) {
			kernels = backend.table
			a := [4]uint64{0, 1, 1 << 31, 0xffffffff}
			b := [4]uint64{0xffffffff, 65535, 0xffffffff, 0xffffffff}
			got := kernels.quantMultiply(a, b)
			want := [4]uint64{0, 65535, 9223372034707292160, 18446744065119617025}
			if got != want {
				t.Fatalf("完整64位乘积=%v，期望%v", got, want)
			}
			rng := rand.New(rand.NewPCG(0x1114, 0x1014))
			for range 1000 {
				var values, mul [4]int64
				for i := range values {
					values[i] = int64(rng.Uint32())
					if rng.IntN(2) == 0 {
						values[i] = -values[i]
					}
					mul[i] = int64(rng.Uint32())
				}
				products := signedProducts4(values, mul)
				for i, v := range products {
					if v != values[i]*mul[i] {
						t.Fatalf("有符号乘积lane%d=%d，期望%d", i, v, values[i]*mul[i])
					}
				}
			}
			for _, v := range []int64{-1 << 35, -1 << 32, -0xffffffff, 0xffffffff, 1 << 32, 1 << 35} {
				values := [4]int64{v, -v, 3, -3}
				mul := [4]int64{29, 255, 1 << 33, 1 << 33}
				out := signedProducts4(values, mul)
				for i, p := range out {
					if p != values[i]*mul[i] {
						t.Fatalf("宽域回退lane%d=%d", i, p)
					}
				}
			}
		})
	}
}

func TestQuantization_PreparedBlocksAllQP(t *testing.T) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		t.Run(backend.name, func(t *testing.T) {
			kernels = backend.table
			for qp := range 64 {
				for _, divisor := range []int64{3, 6} {
					var src4 [16]int32
					var src8 [64]int32
					var mul4 [16]int64
					var mul8 [64]int64
					for i := range src8 {
						src8[i] = int32(i*179 - 4001)
						mul8[i] = int64(20+i%6) * int64(1+i*17%255) << uint(qp/6)
					}
					for i := range src4 {
						src4[i] = src8[i]
						mul4[i] = (13107*16 + int64(i+1)/2) / int64(i+1)
					}
					src4[0], src4[15] = math.MinInt32, math.MaxInt32
					shift := 15 + qp/6
					var q4 [16]int32
					Quant4x4(&q4, &src4, &mul4, shift, divisor)
					for i, v := range q4 {
						want := QuantizeWithDeadZone(src4[i], mul4[i], shift, divisor)
						if v != want {
							t.Fatalf("QP%d divisor%d Quant4 lane%d=%d/%d", qp, divisor, i, v, want)
						}
					}
					var q8 [64]int32
					n, bad := Quant8x8(&q8, &src8, &mul8, divisor)
					if n != 64 || bad != 64 {
						t.Fatalf("QP%d Quant8意外失败%d/%d", qp, n, bad)
					}
					for i, v := range q8 {
						a := int64(src8[i])
						if a < 0 {
							a = -a
						}
						level := (a*64 + mul8[i]/divisor) / mul8[i]
						if src8[i] < 0 {
							level = -level
						}
						if v != int32(level) {
							t.Fatalf("QP%d divisor%d Quant8 lane%d=%d/%d", qp, divisor, i, v, level)
						}
					}
					var d8 [64]int32
					n = Dequant8x8(&d8, &src8, &mul8)
					expected := 64
					for i, v := range src8 {
						value := (int64(v)*mul8[i] + 32) >> 6
						if value < math.MinInt32 || value > math.MaxInt32 {
							expected = i
							break
						}
						if d8[i] != int32(value) {
							t.Fatalf("QP%d Dequant8 lane%d=%d/%d", qp, i, d8[i], value)
						}
					}
					if n != expected {
						t.Fatalf("反量化溢出边界=%d/%d", n, expected)
					}
				}
			}
		})
	}
}

func TestQuantization_LateErrorsAndLocalAllocation(t *testing.T) {
	var src [64]int32
	var mul [64]int64
	for i := range mul {
		mul[i] = 320
		src[i] = 1
	}
	mul[61] = 0
	var out8 [64]int32
	written, bad := Quant8x8(&out8, &src, &mul, 3)
	if written != 61 || bad != 61 {
		t.Fatalf("晚期零矩阵边界=%d/%d", written, bad)
	}
	mul[61] = 320
	src[59] = math.MaxInt32
	mul[59] = 1 << 25
	written = Dequant8x8(&out8, &src, &mul)
	if written != 59 {
		t.Fatalf("晚期溢出边界=%d", written)
	}
	if got := testing.AllocsPerRun(100, func() {
		var s [16]int32
		var m [16]int64
		for i := range m {
			s[i] = 17
			m[i] = 13107
		}
		var out [16]int32
		Quant4x4(&out, &s, &m, 19, 3)
		if out[0] != 0 {
			panic("量化解析值改变")
		}
		var d [16]int32
		n := Dequant4x4(&d, &s, &m)
		if n != 16 || d[0] != (17*13107+32)>>6 {
			panic("反量化解析值改变")
		}
		QuantizeCoefficients(s[:], s[:], 13107, 19, 3)
	}); got != 0 {
		t.Fatalf("局部量化/反量化分配=%v，期望0", got)
	}
}

var quantBenchmarkSink [16]int32

func BenchmarkQuant4x4_4x4(b *testing.B) {
	var src [16]int32
	var mul [16]int64
	for i := range src {
		src[i] = int32(i*277 - 1023)
		mul[i] = 13107
	}
	b.Run("ScalarBefore", func(b *testing.B) {
		for b.Loop() {
			for i, v := range src {
				quantBenchmarkSink[i] = QuantizeWithDeadZone(v, mul[i], 19, 3)
			}
		}
	})
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		b.Run(backend.name, func(b *testing.B) {
			kernels = backend.table
			for b.Loop() {
				Quant4x4(&quantBenchmarkSink, &src, &mul, 19, 3)
			}
		})
	}
}
