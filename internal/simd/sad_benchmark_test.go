package simd

import "testing"

var sadBenchmarkResult int

func benchmarkSAD8(b *testing.B, fn sad8Func) {
	var a, ref [256]byte
	for i := range a {
		a[i], ref[i] = byte(i), byte(i*7)
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
