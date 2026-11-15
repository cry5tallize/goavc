package simd

import (
	"fmt"
	"testing"
)

var quantStatusBenchmarkSink, quantBadBenchmarkSink int

// 本表仅为固定基准夹具，按领域层的QP'/矩阵规则在计时外准备乘子。
// 不测缓存构建；每op只包含一次真实Quant/Dequant包装调用及持久输出。
func BenchmarkQuantPrepared(b *testing.B) {
	saved := kernels
	defer func() { kernels = saved }()
	quantScale := [6][3]int64{{13107, 8066, 5243}, {11916, 7490, 4660}, {10082, 6554, 4194}, {9362, 5825, 3647}, {8192, 5243, 3355}, {7282, 4559, 2893}}
	level4 := [6][3]int64{{10, 13, 16}, {11, 14, 18}, {13, 16, 20}, {14, 18, 23}, {16, 20, 25}, {18, 23, 29}}
	level8 := [6][6]int64{{20, 18, 32, 19, 25, 24}, {22, 19, 35, 21, 28, 26}, {26, 23, 42, 24, 33, 31}, {28, 25, 45, 26, 35, 33}, {32, 28, 51, 30, 40, 38}, {36, 32, 58, 34, 46, 43}}
	class8 := [16]int{0, 3, 4, 3, 3, 1, 5, 1, 4, 5, 2, 5, 3, 1, 5, 1}
	for _, backend := range guardBackends() {
		name := benchmarkBackendName(b, backend, backend.table.backends.QuantMultiply)
		if name == "" {
			continue
		}
		for _, scenario := range []struct {
			name         string
			qp           int
			custom, wide bool
			divisor      int64
		}{
			{"QP0FlatIntra", 0, false, false, 3},
			{"QP26FlatInter", 26, false, false, 6},
			{"QP26CustomIntra", 26, true, false, 3},
			{"QP51FlatInter", 51, false, false, 6},
			{"QP63CustomInter", 63, true, false, 6},
			{"WideDomain", 26, false, true, 6},
		} {
			b.Run(fmt.Sprintf("%s/%s", scenario.name, name), func(b *testing.B) {
				var src4, levels4 [16]int32
				var src8, levels8 [64]int32
				var qmul4, dmul4 [16]int64
				var mul8 [64]int64
				shift := 15 + scenario.qp/6
				for i := range src8 {
					src8[i], levels8[i] = int32(i*179-4001), int32(i%63-31)
					matrix := int64(16)
					if scenario.custom {
						matrix = int64(1 + i*17%255)
					}
					mul8[i] = level8[scenario.qp%6][class8[(i/8%4)*4+i%4]] * matrix << uint(scenario.qp/6)
					if scenario.wide {
						src8[i] = int32(i%2)*(1<<31-1) - int32(1-i%2)*(1<<31-1)
						levels8[i], mul8[i] = int32(i%15-7), 1<<33
					}
				}
				for i := range src4 {
					src4[i], levels4[i] = src8[i], levels8[i]
					matrix := int64(16)
					if scenario.custom {
						matrix = int64(1 + i*17%255)
					}
					class := (i / 4 % 2) + i%2
					qmul4[i] = (quantScale[scenario.qp%6][class]*16 + matrix/2) / matrix
					dmul4[i] = level4[scenario.qp%6][class] * matrix << uint(scenario.qp/6+2)
					if scenario.wide {
						// 大乘子真实触发32×32乘积慢路，保持int64乘积及输出合法。
						src4[i] = int32(i%15 - 7)
						qmul4[i], dmul4[i], shift = 1<<33, 1<<33, 35
					}
				}
				var wantQ4, wantD4 [16]int32
				var wantQ8, wantD8 [64]int32
				kernels = genericTable()
				Quant4x4(&wantQ4, &src4, &qmul4, shift, scenario.divisor)
				wantQN, wantBad := Quant8x8(&wantQ8, &src8, &mul8, scenario.divisor)
				wantD4N := Dequant4x4(&wantD4, &levels4, &dmul4)
				wantD8N := Dequant8x8(&wantD8, &levels8, &mul8)
				if wantQN != 64 || wantBad != 64 || wantD4N != 16 || wantD8N != 64 {
					b.Fatal("基准夹具超出合法输出域")
				}
				kernels = backend.table
				quant4Name, dequant4Name, dequant8Name := "Quant4", "Dequant4", "Dequant8"
				if scenario.wide {
					quant4Name += "-Int64Fallback"
					dequant4Name += "-Int64Fallback"
					dequant8Name += "-Int64Fallback"
				}
				b.Run(quant4Name, func(b *testing.B) {
					var got [16]int32
					Quant4x4(&got, &src4, &qmul4, shift, scenario.divisor)
					if got != wantQ4 {
						b.Fatal("Quant4预校验失败")
					}
					b.ReportAllocs()
					for b.Loop() {
						Quant4x4(&transformSmallBenchmarkSink, &src4, &qmul4, shift, scenario.divisor)
					}
				})
				b.Run("Quant8", func(b *testing.B) {
					var got [64]int32
					n, bad := Quant8x8(&got, &src8, &mul8, scenario.divisor)
					if got != wantQ8 || n != wantQN || bad != wantBad {
						b.Fatal("Quant8预校验失败")
					}
					b.ReportAllocs()
					for b.Loop() {
						quantStatusBenchmarkSink, quantBadBenchmarkSink = Quant8x8(&quantWideBenchSink, &src8, &mul8, scenario.divisor)
					}
				})
				b.Run(dequant4Name, func(b *testing.B) {
					var got [16]int32
					if n := Dequant4x4(&got, &levels4, &dmul4); n != wantD4N || got != wantD4 {
						b.Fatal("Dequant4预校验失败")
					}
					b.ReportAllocs()
					for b.Loop() {
						quantStatusBenchmarkSink = Dequant4x4(&transformSmallBenchmarkSink, &levels4, &dmul4)
					}
				})
				b.Run(dequant8Name, func(b *testing.B) {
					var got [64]int32
					if n := Dequant8x8(&got, &levels8, &mul8); n != wantD8N || got != wantD8 {
						b.Fatal("Dequant8预校验失败")
					}
					b.ReportAllocs()
					for b.Loop() {
						quantStatusBenchmarkSink = Dequant8x8(&quantWideBenchSink, &levels8, &mul8)
					}
				})
			})
		}
	}
}
