//go:build goavc_simdtest

package filter

import (
	"errors"
	"testing"

	"github.com/cry5tallize/goavc/internal/simd"
)

// BenchmarkPreparedLumaEdge_16_SSE2 复用A00同一整边夹具，只强制亮度整边槽。
func BenchmarkPreparedLumaEdge_16_SSE2(b *testing.B) {
	restore, err := simd.ForceLuma8EdgeBackendForTesting(simd.SSE2)
	if errors.Is(err, simd.ErrBackendUnavailable) {
		b.Skip("当前构建或CPU没有亮度整边SSE2，未计入该后端性能证据")
	}
	if err != nil {
		b.Fatal(err)
	}
	defer restore()
	benchmarkPreparedEdge(b, false)
}

func TestPreparedEdge_Luma8SSE2DispatchAndFallback(t *testing.T) {
	restore, err := simd.ForceLuma8EdgeBackendForTesting(simd.SSE2)
	if errors.Is(err, simd.ErrBackendUnavailable) {
		t.Skip("当前构建或CPU没有亮度整边SSE2，未计入该后端通过")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if got := simd.ActiveBackends().FilterLumaEdge; got != simd.SSE2 {
		t.Fatalf("整边后端=%s，期望SSE2", got)
	}
	for _, geometry := range []struct {
		q0, lineStep, acrossStep, size int
	}{
		{4*19 + 4, 19, 1, 19 * 21}, // 完整垂直边。
		{4*21 + 4, 1, 21, 21 * 21}, // 完整水平边。
		{3, 1, 1, 20},              // q2不完整，逐段精确回退。
	} {
		var storage [21 * 21]byte
		for i := range storage {
			storage[i] = byte(i*41 + i/5 + 7)
		}
		got := append([]byte(nil), storage[:geometry.size]...)
		want := append([]byte(nil), got...)
		p := PrepareEdge(30, 0, 0, 8)
		strengths := [4]uint8{1, 4, 3, 0}
		p.Luma8(got, geometry.q0, geometry.lineStep, geometry.acrossStep, strengths)
		for seg, bs := range strengths {
			if bs == 0 {
				continue
			}
			LumaEdge(want, geometry.q0+seg*4*geometry.lineStep, geometry.lineStep,
				geometry.acrossStep, int(bs), 30, 0, 0)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("geometry=%+v bS=%v byte[%d]=%d，期望%d", geometry, strengths, i, got[i], want[i])
			}
		}
	}
}
