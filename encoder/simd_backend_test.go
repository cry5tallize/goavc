//go:build goavc_simdtest

package encoder

import (
	"errors"
	"github.com/goavc/h264/internal/simd"
	"testing"
)

func TestParity_IntraBackends(t *testing.T) {
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
			if simd.ActiveBackends().SATD4x4 != backend {
				t.Skip("该后端SATD槽未实现；不将Generic回退记为该后端通过")
			}
			TestIntraSelection_AllModes(t)
			TestHigh10ModeSearch_PreservesAllIntraModes(t)
			TestIntra_FrozenCandidates(t)
			TestAllocation_IntraCandidate(t)
			TestNeighbors_ScratchLifetimeAndUnavailable(t)
		})
	}
}

func TestParity_ResidualBackends(t *testing.T) {
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
			TestResidual_FrozenCandidates(t)
			TestAllocation_InterResidual(t)
			TestAllocation_InterCandidate(t)
		})
	}
}
