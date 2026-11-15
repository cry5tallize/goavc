//go:build goavc_simdtest

package search

import (
	"errors"
	"testing"

	"github.com/goavc/h264/internal/simd"
)

func TestParity_SearchBackends(t *testing.T) {
	for _, backend := range []simd.Backend{simd.Generic, simd.SSE2, simd.AVX2, simd.NEON} {
		t.Run(backend.String(), func(t *testing.T) {
			restore, err := simd.ForceBackendForTesting(backend)
			if errors.Is(err, simd.ErrBackendUnavailable) {
				t.Skip("后端未实现或CPU不能安全执行；记为未执行")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer restore()
			TestSAD_ExtremesAndUnalignedStride(t)
			TestSearch_TranslationAndSubpixel(t)
			TestSearch_DiamondAndBounds(t)
			TestSearch_CostCallbackPreservesReportedSAD(t)
		})
	}
}
