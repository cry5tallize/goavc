//go:build amd64 && !purego

package simd

import "testing"

func TestForward_SSE2(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("CPU不支持SSE2，未执行")
	}
	checkForward(t, forward4SSE2)
}
func BenchmarkForward4x4_4x4_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("CPU不支持SSE2")
	}
	benchForward(b, forward4SSE2)
}
