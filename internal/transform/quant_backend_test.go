//go:build goavc_simdtest

package transform

import (
	"errors"
	"testing"

	"github.com/goavc/h264/internal/simd"
)

func TestQuantization_Backends(t *testing.T) {
	for _, backend := range []simd.Backend{simd.Generic, simd.SSE2, simd.AVX2, simd.NEON} {
		t.Run(backend.String(), func(t *testing.T) {
			restore, err := simd.ForceBackendForTesting(backend)
			if errors.Is(err, simd.ErrBackendUnavailable) {
				t.Skip("该后端不可执行，不计作通过")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer restore()
			if simd.ActiveBackends().QuantMultiply != backend {
				t.Fatal("未选择请求的真实量化乘法后端")
			}
			TestHigh10Dequant_AnalyticQP52To63(t)
			TestHigh10Quant_AnalyticQP63DeadZones(t)
			TestHigh10DC_Int64TemporaryAndOverflow(t)
			TestHigh10Quant_AllEffectiveQPsAndScaling(t)
			TestHigh10Kernel_InvalidEffectiveQP(t)
			TestInterQuantization_AnalyticDeadZone(t)
			TestInterQuantization_ScalingAndInvalidQP(t)
			TestDequant8x8_NonDCScalingAnalyticOutput(t)
			TestQuant8x8WithScaling_ParityAndBounds(t)
			TestChromaDC422_ScalingAndQPOffset(t)
			TestChromaDC422_ExtremesAliasAndRejection(t)
			TestDequantization_FirstFailurePreservesPrefix(t)
			TestQuant8x8_LateFailurePreservesAliasedSuffix(t)
		})
	}
}
