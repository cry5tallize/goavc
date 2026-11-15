//go:build goavc_simdtest

package simd

import "testing"

func BenchmarkForward4x4Dispatch_4x4_Generic(b *testing.B) {
	benchForwardDispatch(b, forward4Generic, Generic)
}
func BenchmarkForward4x4Dispatch_4x4_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("SSE2不可用")
	}
	benchForwardDispatch(b, selectKernels(cpuFeatures{sse2: true}).forward4, SSE2)
}

func benchForwardDispatch(b *testing.B, fn forward4Func, backend Backend) {
	backendTestMu.Lock()
	previous := kernels
	kernels.forward4, kernels.backends.Forward4x4 = fn, backend
	defer func() { kernels = previous; backendTestMu.Unlock() }()
	benchForward(b, Forward4x4)
}
