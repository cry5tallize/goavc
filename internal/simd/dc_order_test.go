package simd

import "testing"

// Spec: 8.5.11。独立2×2 Hadamard解析值区分水平与垂直DC，
// 混合符号不能因两个零系数恰好相等而掩盖SSE2通道交换。
func TestForwardChromaDC_AnalyticMixedSigns(t *testing.T) {
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		t.Run(backend.name, func(t *testing.T) {
			kernels = backend.table
			for _, tc := range []struct{ src, want [4]int32 }{
				{[4]int32{1023, -1023, 511, -511}, [4]int32{0, 3068, 0, 1024}},
				{[4]int32{-1023, 1023, -511, 511}, [4]int32{0, -3068, 0, -1024}},
				{[4]int32{1, 2, 3, 4}, [4]int32{10, -2, -4, 0}},
				{[4]int32{1<<31 - 1, 1<<31 - 1, 1<<31 - 1, 1<<31 - 1}, [4]int32{-4, 0, 0, 0}},
			} {
				if got := ForwardChromaDC(tc.src); got != tc.want {
					t.Fatalf("src=%v DC=%v，期望%v", tc.src, got, tc.want)
				}
			}
		})
	}
}
