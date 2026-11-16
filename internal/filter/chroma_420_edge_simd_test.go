//go:build goavc_simdtest

package filter

import (
	"errors"
	"testing"

	"github.com/cry5tallize/goavc/internal/simd"
)

func TestChroma420Edge_ForcedSlots(t *testing.T) {
	for _, backend := range []simd.Backend{simd.Generic, simd.SSE2} {
		before := simd.ActiveBackends()
		restore, err := simd.ForceChroma8EdgeBackendForTesting(backend)
		if errors.Is(err, simd.ErrBackendUnavailable) {
			t.Logf("%s不可用，未计入该后端通过", backend)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer restore()
			actual := simd.ActiveBackends()
			if actual.FilterChromaEdge != backend || actual.FilterLumaEdge != before.FilterLumaEdge || actual.FilterDelta != before.FilterDelta {
				t.Fatalf("色度槽切换影响其他后端：before=%+v after=%+v", before, actual)
			}
			t.Logf("FilterChromaEdge=%s FilterLumaEdge=%s FilterDelta=%s", actual.FilterChromaEdge, actual.FilterLumaEdge, actual.FilterDelta)
			for _, steps := range [][2]int{{29, 1}, {1, 29}} {
				for _, c := range edgeBenchmarkCases {
					checkEdgeBenchmarkFixture(t, c, 8, steps[0], steps[1], 2, true)
				}
			}
			TestChroma420Edge_FallbackAndAllocations(t)
		}()
		if simd.ActiveBackends() != before {
			t.Fatal("色度槽未恢复")
		}
	}
}
