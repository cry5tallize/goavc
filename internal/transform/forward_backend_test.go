//go:build goavc_simdtest

package transform

import (
	"errors"
	"github.com/goavc/h264/internal/simd"
	"testing"
)

func TestForward_Backends(t *testing.T) {
	for _, backend := range []simd.Backend{simd.Generic, simd.SSE2, simd.AVX2, simd.NEON} {
		t.Run(backend.String(), func(t *testing.T) {
			restore, err := simd.ForceBackendForTesting(backend)
			if errors.Is(err, simd.ErrBackendUnavailable) {
				t.Skip("后端不可用，未执行")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer restore()
			if simd.ActiveBackends().Forward4x4 != backend {
				t.Skip("此Forward槽未实现，不把回退当原生通过")
			}
			TestForward4x4_AnalyticBasis(t)
			TestForward_InPlaceAndLocalAllocation(t)
		})
	}
}

func BenchmarkForwardConsumer_4x4_Generic(b *testing.B) { benchForwardConsumer(b, simd.Generic) }
func BenchmarkForwardConsumer_4x4_SSE2(b *testing.B)    { benchForwardConsumer(b, simd.SSE2) }
func benchForwardConsumer(b *testing.B, backend simd.Backend) {
	restore, err := simd.ForceBackendForTesting(backend)
	if errors.Is(err, simd.ErrBackendUnavailable) {
		b.Skip("后端不可用")
	}
	if err != nil {
		b.Fatal(err)
	}
	defer restore()
	var src, out [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		Forward4x4(&out, &src)
	}
	b.StopTimer()
	if out[0] == 0 {
		b.Fatal("empty transform")
	}
}
