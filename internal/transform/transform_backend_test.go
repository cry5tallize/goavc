//go:build goavc_simdtest

package transform

import (
	"errors"
	"testing"

	"github.com/goavc/h264/internal/simd"
)

func TestTransforms_BackendContracts(t *testing.T) {
	for _, backend := range []simd.Backend{simd.Generic, simd.SSE2, simd.AVX2, simd.NEON} {
		t.Run(backend.String(), func(t *testing.T) {
			restore, err := simd.ForceBackendForTesting(backend)
			if errors.Is(err, simd.ErrBackendUnavailable) {
				t.Skip("该后端不可执行，不计作原生通过")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer restore()
			selected := simd.ActiveBackends()
			for _, slot := range []simd.Backend{selected.Forward4x4, selected.Forward8x8, selected.IDCT4x4, selected.IDCT8x8, selected.ForwardLumaDC, selected.ForwardChromaDC, selected.ForwardChromaDC422} {
				if slot != backend {
					t.Fatalf("变换未选择请求的真实后端：请求%v，表%+v", backend, selected)
				}
			}
			TestForward4x4_AnalyticBasis(t)
			TestForwardDC_AnalyticBasis(t)
			TestIDCT4x4_MixedOddCoefficientsRespectPassOrder(t)
			TestIDCT4x4_LargeDCDoesNotWrap(t)
			TestIDCT4x4_DCOnly(t)
			TestIDCT4x4_Impulse(t)
			TestIDCT4x4_ConstantDCBasis(t)
			TestIDCT8x8_AnalyticBases(t)
			TestForward8x8_AnalyticConstantAndImpulse(t)
			TestForward8x8_OrthogonalSpatialBases(t)
			TestHigh10Forward_ExtremeResidualMatrix(t)
			TestHigh10Forward8x8_ExtremeImpulseAnalyticBasis(t)
			TestChromaDC422_KnownSpatialBases(t)
			TestChromaDC422_ExtremesAliasAndRejection(t)
			if alloc := testing.AllocsPerRun(100, func() {
				var four [16]int32
				var eight [64]int32
				for i := range four {
					four[i] = 7
				}
				for i := range eight {
					eight[i] = 7
				}
				Forward4x4(&four, &four)
				IDCT4x4(&four, &four)
				Forward8x8(&eight, &eight)
				if err := IDCT8x8(&eight, &eight); err != nil {
					panic("合法逆变换溢出")
				}
				for _, v := range four {
					if v != 2 {
						panic("4×4原地解析值改变")
					}
				}
				for _, v := range eight {
					if v != 7 {
						panic("8×8原地解析值改变")
					}
				}
			}); alloc != 0 {
				t.Fatalf("领域局部正逆变换分配=%v，期望0", alloc)
			}
		})
	}
}
