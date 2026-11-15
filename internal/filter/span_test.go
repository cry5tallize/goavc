package filter

import "testing"

func TestLumaSpanOneAndAllocations(t *testing.T) {
	var data [32]uint16
	for i := range data {
		data[i] = 500
	}
	data[4], data[12], data[20], data[28] = 504, 504, 504, 504
	want := data
	LumaEdgeHigh(want[:], 4, 8, 1, 4, 30, 0, 0, 10)
	for line := range 4 {
		LumaEdgeHighSpan(data[:], 4+8*line, 8, 1, 4, 30, 0, 0, 1, 10)
	}
	if data != want {
		t.Fatalf("单线段与四线段滤波结果不一致：得到 %v，期望 %v", data, want)
	}
	if n := testing.AllocsPerRun(100, func() {
		LumaEdgeHighSpan(data[:], 4, 8, 1, 1, 30, 0, 0, 1, 10)
		ChromaEdgeHigh(data[:], 4, 8, 1, 4, 30, 0, 0, 1, 10)
	}); n != 0 {
		t.Fatalf("去块滤波产生分配：%v", n)
	}
}

func BenchmarkFilterSpan(b *testing.B) {
	var data [32]uint16
	for i := range data {
		data[i] = uint16(500 + i%8)
	}
	b.ReportAllocs()
	for b.Loop() {
		LumaEdgeHighSpan(data[:], 4, 8, 1, 1, 30, 0, 0, 4, 10)
	}
}
