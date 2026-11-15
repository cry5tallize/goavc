//go:build amd64 && !purego

package simd

import "testing"

func TestForward_SSE2(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("CPU不支持SSE2，未执行")
	}
	checkForward(t, forward4SSE2)
}

func TestTransform_SSE2Parity(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("CPU不支持SSE2，未执行")
	}
	checkTransformBackends(t, quantMultiplySSE2, wideAdd4SSE2, forwardChromaDCSSE2)
}

func TestTransform_AVX2Parity(t *testing.T) {
	if !nativeCPU.avx2 {
		t.Skip("CPU不支持AVX2，未执行")
	}
	checkForward(t, forward4AVX2)
	checkTransformBackends(t, quantMultiplyAVX2, wideAdd4AVX2, forwardChromaDCAVX2)
}

func checkTransformBackends(t *testing.T, multiply quantMultiplyFunc, add wideAdd4Func, chromaKernel func([4]int32) [4]int32) {
	t.Helper()
	var src8 [64]int32
	for i := range src8 {
		src8[i] = int32((i*7919)%2047 - 1023)
	}
	if got, want := forward8Vectorized(src8, multiply), forward8Generic(src8); got != want {
		t.Fatal("Forward8x8 backend differs from Generic")
	}
	var src4 [16]int32
	for i := range src4 {
		src4[i] = int32(i*7919 - 250000)
	}
	if got, want := idct4Vectorized(src4, add), idct4Generic(src4); got != want {
		t.Fatal("IDCT4x4 backend differs from Generic")
	}
	var srcIDCT8 [64]int32
	for i := range srcIDCT8 {
		srcIDCT8[i] = int32(i*7919 - 250000)
	}
	got, gotWritten := idct8Vectorized(srcIDCT8, add)
	want, wantWritten := idct8Generic(srcIDCT8)
	if gotWritten != wantWritten || got != want {
		t.Fatal("IDCT8x8 backend differs from Generic")
	}
	var luma [16]int32
	for i := range luma {
		luma[i] = int32(i*47 - 327)
	}
	if got, want := forwardLumaDCVectorized(luma, add), forwardLumaDCGeneric(luma); got != want {
		t.Fatal("luma DC backend differs from Generic")
	}
	var dc422 [8]int32
	for i := range dc422 {
		dc422[i] = int32(i*91 - 318)
	}
	if got, want := forwardChromaDC422Vectorized(dc422, add), forwardChromaDC422Generic(dc422); got != want {
		t.Fatal("422 DC backend differs from Generic")
	}
	chroma := [4]int32{1023, -1023, 511, -511}
	if got, want := chromaKernel(chroma), forwardChromaDCGeneric(chroma); got != want {
		t.Fatal("420 DC backend differs from Generic")
	}
}

func TestTransformNative_ZeroAllocs(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("CPU不支持SSE2，未执行")
	}
	var src4 [16]int32
	var src8 [64]int32
	for i := range src4 {
		src4[i] = int32(i - 8)
	}
	for i := range src8 {
		src8[i] = int32(i - 32)
	}
	if n := testing.AllocsPerRun(100, func() {
		_ = forward4SSE2(src4)
		_ = forward8SSE2(src8)
		_ = idct4SSE2(src4)
		_, _ = idct8SSE2(src8)
		_ = forwardLumaDCSSE2(src4)
		_ = forwardChromaDCSSE2([4]int32{1, -1, 2, -2})
		_ = forwardChromaDC422SSE2([8]int32{1, -1, 2, -2, 3, -3, 4, -4})
	}); n != 0 {
		t.Fatalf("native transform allocated: %v", n)
	}
}
func BenchmarkForward4x4_4x4_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("CPU不支持SSE2")
	}
	benchForward(b, forward4SSE2)
}

func BenchmarkForward4x4_AVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("CPU不支持AVX2")
	}
	benchForward(b, forward4AVX2)
}

func BenchmarkForward8x8_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("CPU不支持SSE2")
	}
	benchForward8Backend(b, quantMultiplySSE2)
}
func BenchmarkForward8x8_AVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("CPU不支持AVX2")
	}
	benchForward8Backend(b, quantMultiplyAVX2)
}

func BenchmarkIDCT4x4_SSE2(b *testing.B) { benchIDCT4Backend(b, wideAdd4SSE2) }
func BenchmarkIDCT4x4_AVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("CPU不支持AVX2")
	}
	benchIDCT4Backend(b, wideAdd4AVX2)
}

func benchIDCT4Backend(b *testing.B, add wideAdd4Func) {
	var src [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	for range b.N {
		_ = idct4Vectorized(src, add)
	}
}

func BenchmarkIDCT8x8_AVX2(b *testing.B) {
	if !nativeCPU.avx2 {
		b.Skip("CPU不支持AVX2")
	}
	var src [64]int32
	for i := range src {
		src[i] = int32(i*31 - 960)
	}
	b.ReportAllocs()
	for range b.N {
		_, _ = idct8Vectorized(src, wideAdd4AVX2)
	}
}

func BenchmarkIDCT8x8_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("CPU不支持SSE2")
	}
	var src [64]int32
	for i := range src {
		src[i] = int32(i*31 - 960)
	}
	b.ReportAllocs()
	for range b.N {
		_, _ = idct8SSE2(src)
	}
}

func BenchmarkForwardDC_SSE2(b *testing.B) {
	if !nativeCPU.sse2 {
		b.Skip("CPU不支持SSE2")
	}
	var src [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	for range b.N {
		_ = forwardLumaDCSSE2(src)
	}
}

func benchForward8Backend(b *testing.B, multiply quantMultiplyFunc) {
	var src, out [64]int32
	for i := range src {
		src[i] = int32(i*31 - 960)
	}
	b.ReportAllocs()
	for range b.N {
		out = forward8Vectorized(src, multiply)
	}
	if out[0] == 0 {
		b.Fatal("empty transform")
	}
}
