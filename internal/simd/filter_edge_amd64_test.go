//go:build amd64 && !purego

package simd

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// Samples are ordered p3, p2, p1, p0, q0, q1, q2, q3.
type filterEdgeFixture struct {
	name    string
	alpha   int
	beta    int
	tc      [4]int
	samples [16][8]byte
}

type filterEdgeGeometry struct {
	name       string
	q0         int
	lineStep   int
	acrossStep int
	size       int
}

func filterEdgeGeometries() []filterEdgeGeometry {
	const verticalStride = 37
	const horizontalStride = 29
	return []filterEdgeGeometry{
		{"vertical", 13, verticalStride, 1, 13 + 15*verticalStride + 3 + 17},
		// The first p3 and last q3 are outside the slice, but all p2/q2 exist.
		{"vertical-tight", 3, verticalStride, 1, 15*verticalStride + 6},
		{"horizontal", 4*horizontalStride + 5, 1, horizontalStride, 7*horizontalStride + 37},
		{"horizontal-no-p3", 3*horizontalStride + 3, 1, horizontalStride, 6*horizontalStride + 31},
		{"horizontal-no-q3", 4*horizontalStride + 3, 1, horizontalStride, 6*horizontalStride + 19},
		{"horizontal-no-p3-q3", 3*horizontalStride + 3, 1, horizontalStride, 5*horizontalStride + 19},
	}
}

func filterEdgeRows(rows ...[8]byte) [16][8]byte {
	var samples [16][8]byte
	for line := range samples {
		samples[line] = rows[line%len(rows)]
	}
	return samples
}

func filterEdgeInput(g filterEdgeGeometry, samples [16][8]byte) []byte {
	data := make([]byte, g.size)
	// Nonconstant surrounding bytes make unintended stores visible in the
	// complete-buffer comparison, including padding between vertical lines.
	for i := range data {
		data[i] = byte(i*73 + i/7 + 19)
	}
	for line, row := range samples {
		q0 := g.q0 + line*g.lineStep
		for sample, value := range row {
			index := q0 + (sample-4)*g.acrossStep
			if index >= 0 && index < len(data) {
				data[index] = value
			}
		}
	}
	return data
}

func checkFilterLuma8EdgeSSE2(t *testing.T, g filterEdgeGeometry, fixture filterEdgeFixture, strengths [4]uint8) {
	t.Helper()
	got := filterEdgeInput(g, fixture.samples)
	want := append([]byte(nil), got...)
	for segment, strength := range strengths {
		// filterLuma itself assumes a nonzero bS. A zero-strength segment
		// must leave its original samples untouched.
		if strength == 0 {
			continue
		}
		tc := 0
		if strength < 4 {
			tc = fixture.tc[strength]
		}
		filterLuma(want, g.q0+segment*4*g.lineStep, g.lineStep, g.acrossStep,
			int(strength), fixture.alpha, fixture.beta, tc, 4, 8, nil)
	}
	filterLuma8EdgeSSE2(got, g.q0, g.lineStep, g.acrossStep, strengths,
		fixture.alpha, fixture.beta, fixture.tc)
	for i, expected := range want {
		if got[i] != expected {
			t.Fatalf("strengths=%v alpha=%d beta=%d tc=%v byte[%d]=%d, scalar=%d",
				strengths, fixture.alpha, fixture.beta, fixture.tc, i, got[i], expected)
		}
	}
}

func TestFilterLuma8EdgeSSE2Parity(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("CPU does not support SSE2")
	}
	fixtures := []filterEdgeFixture{
		{
			name: "weak-snapshots-and-side-clipping", alpha: 40, beta: 20, tc: [4]int{0, 1, 2, 4},
			samples: filterEdgeRows(
				[8]byte{88, 92, 96, 100, 110, 114, 118, 125},
				[8]byte{125, 118, 114, 110, 100, 96, 92, 88},
				[8]byte{60, 80, 96, 100, 110, 114, 118, 130},
				[8]byte{70, 92, 96, 100, 110, 114, 130, 150},
				[8]byte{60, 80, 96, 100, 110, 114, 130, 150},
				[8]byte{88, 92, 103, 100, 109, 106, 117, 125},
			),
		},
		{
			name: "zero-tc-still-filters-p0-q0", alpha: 40, beta: 20, tc: [4]int{},
			samples: filterEdgeRows(
				[8]byte{88, 92, 96, 100, 110, 114, 118, 125},
				[8]byte{125, 118, 114, 110, 100, 96, 92, 88},
				[8]byte{60, 80, 96, 100, 110, 114, 118, 130},
				[8]byte{60, 80, 96, 100, 110, 114, 130, 150},
			),
		},
		{
			name: "pixel-clipping-and-signed-rounding", alpha: 40, beta: 20, tc: [4]int{0, 1, 3, 7},
			samples: filterEdgeRows(
				[8]byte{0, 0, 10, 0, 1, 0, 1, 0},
				[8]byte{255, 254, 255, 254, 255, 245, 255, 255},
				[8]byte{0, 1, 0, 1, 0, 10, 0, 0},
				[8]byte{255, 255, 245, 255, 254, 255, 254, 255},
				[8]byte{88, 94, 99, 101, 100, 100, 108, 115},
				[8]byte{88, 94, 100, 100, 101, 99, 108, 115},
			),
		},
		{
			name: "outer-threshold-boundaries", alpha: 20, beta: 10, tc: [4]int{0, 1, 2, 3},
			samples: filterEdgeRows(
				[8]byte{90, 100, 100, 100, 119, 119, 119, 130},
				[8]byte{90, 100, 100, 100, 120, 120, 120, 130},
				[8]byte{90, 100, 100, 100, 121, 121, 121, 130},
				[8]byte{130, 119, 119, 119, 100, 100, 100, 90},
				[8]byte{130, 120, 120, 120, 100, 100, 100, 90},
				[8]byte{130, 121, 121, 121, 100, 100, 100, 90},
				[8]byte{90, 100, 109, 100, 105, 105, 105, 120},
				[8]byte{90, 100, 110, 100, 105, 105, 105, 120},
				[8]byte{90, 100, 111, 100, 105, 105, 105, 120},
				[8]byte{90, 100, 91, 100, 105, 105, 105, 120},
				[8]byte{90, 100, 90, 100, 105, 105, 105, 120},
				[8]byte{90, 100, 89, 100, 105, 105, 105, 120},
				[8]byte{90, 100, 100, 100, 105, 114, 105, 120},
				[8]byte{90, 100, 100, 100, 105, 115, 105, 120},
				[8]byte{90, 100, 100, 100, 105, 116, 105, 120},
				[8]byte{90, 100, 100, 100, 105, 96, 105, 120},
			),
		},
		{
			name: "side-threshold-boundaries", alpha: 40, beta: 20, tc: [4]int{0, 1, 2, 5},
			samples: filterEdgeRows(
				[8]byte{60, 81, 98, 100, 105, 107, 124, 160},
				[8]byte{60, 80, 98, 100, 105, 107, 124, 160},
				[8]byte{60, 79, 98, 100, 105, 107, 124, 160},
				[8]byte{60, 81, 98, 100, 105, 107, 125, 160},
				[8]byte{60, 81, 98, 100, 105, 107, 126, 160},
				[8]byte{160, 119, 102, 100, 105, 103, 86, 60},
				[8]byte{160, 120, 102, 100, 105, 103, 85, 60},
				[8]byte{160, 121, 102, 100, 105, 103, 84, 60},
			),
		},
		{
			name: "strong-inner-threshold-and-neighbors", alpha: 40, beta: 20, tc: [4]int{0, 1, 2, 3},
			samples: filterEdgeRows(
				[8]byte{60, 94, 98, 100, 105, 106, 112, 150},
				[8]byte{60, 94, 98, 100, 111, 112, 118, 150},
				[8]byte{60, 94, 98, 100, 112, 113, 119, 150},
				[8]byte{60, 94, 98, 100, 113, 114, 120, 150},
				[8]byte{150, 118, 112, 111, 100, 98, 94, 60},
				[8]byte{150, 119, 113, 112, 100, 98, 94, 60},
				[8]byte{150, 120, 114, 113, 100, 98, 94, 60},
				[8]byte{60, 80, 98, 100, 105, 106, 112, 150},
				[8]byte{60, 94, 98, 100, 105, 106, 125, 150},
				[8]byte{60, 80, 98, 100, 105, 106, 125, 150},
			),
		},
		{
			name: "zero-alpha", alpha: 0, beta: 20, tc: [4]int{0, 1, 2, 3},
			samples: filterEdgeRows([8]byte{60, 94, 98, 100, 105, 106, 112, 150}),
		},
		{
			name: "zero-beta", alpha: 40, beta: 0, tc: [4]int{0, 1, 2, 3},
			samples: filterEdgeRows([8]byte{60, 94, 100, 100, 105, 105, 112, 150}),
		},
		{
			name: "max-thresholds", alpha: 255, beta: 255, tc: [4]int{0, 1, 13, 25},
			samples: filterEdgeRows(
				[8]byte{255, 0, 255, 0, 254, 0, 255, 0},
				[8]byte{0, 255, 0, 255, 1, 255, 0, 255},
				[8]byte{0, 0, 0, 0, 255, 255, 255, 255},
			),
		},
	}
	strengthSets := [][4]uint8{
		{0, 0, 0, 0},
		{1, 1, 1, 1},
		{2, 2, 2, 2},
		{3, 3, 3, 3},
		{4, 4, 4, 4},
		{0, 1, 2, 3},
		{4, 0, 3, 1},
		{1, 4, 2, 4},
		{0, 4, 0, 4},
		{5, 255, 1, 3},
	}
	for _, g := range filterEdgeGeometries() {
		for _, fixture := range fixtures {
			t.Run(g.name+"/"+fixture.name, func(t *testing.T) {
				for _, strengths := range strengthSets {
					checkFilterLuma8EdgeSSE2(t, g, fixture, strengths)
				}
			})
		}
	}
}

func TestFilterLuma8EdgeSSE2DeterministicParity(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("CPU does not support SSE2")
	}
	rng := rand.New(rand.NewPCG(0xA018, 0x5EED))
	geometries := filterEdgeGeometries()
	for iteration := range 256 {
		g := geometries[iteration%len(geometries)]
		fixture := filterEdgeFixture{
			alpha: rng.IntN(256), beta: rng.IntN(32),
			tc: [4]int{0, rng.IntN(4), rng.IntN(8), rng.IntN(26)},
		}
		var strengths [4]uint8
		for segment := range strengths {
			strengths[segment] = uint8(rng.IntN(5))
		}
		for line := range fixture.samples {
			base := 32 + rng.IntN(192)
			for sample := range fixture.samples[line] {
				if iteration%2 == 0 {
					// Correlated neighborhoods exercise successful filters;
					// independent pixels also exercise masks and rejection.
					fixture.samples[line][sample] = byte(base + rng.IntN(33) - 16)
				} else {
					fixture.samples[line][sample] = byte(rng.IntN(256))
				}
			}
		}
		t.Run(fmt.Sprintf("%s/%03d", g.name, iteration), func(t *testing.T) {
			checkFilterLuma8EdgeSSE2(t, g, fixture, strengths)
		})
	}
}

func TestFilterLuma8EdgeSSE2NoAllocations(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("CPU does not support SSE2")
	}
	fixture := filterEdgeFixture{
		alpha: 40, beta: 20, tc: [4]int{0, 0, 2, 4},
		samples: filterEdgeRows(
			[8]byte{60, 94, 98, 100, 105, 106, 112, 150},
			[8]byte{150, 112, 106, 105, 100, 98, 94, 60},
		),
	}
	strengthSets := [][4]uint8{{0, 0, 0, 0}, {1, 2, 3, 4}, {4, 4, 4, 4}}
	for _, g := range filterEdgeGeometries() {
		t.Run(g.name, func(t *testing.T) {
			input := filterEdgeInput(g, fixture.samples)
			for _, strengths := range strengthSets {
				allocs := testing.AllocsPerRun(100, func() {
					var data [1024]byte
					copy(data[:], input)
					filterLuma8EdgeSSE2(data[:len(input)], g.q0, g.lineStep, g.acrossStep, strengths,
						fixture.alpha, fixture.beta, fixture.tc)
				})
				if allocs != 0 {
					t.Fatalf("strengths=%v allocated %g times per edge, want 0", strengths, allocs)
				}
			}
		})
	}
}

func TestFilterLuma8EdgeSSE2SkipsPixelAccessWhenInactive(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU不能执行SSE2")
	}
	filterLuma8EdgeSSE2(nil, -1, 0, 0, [4]uint8{4, 4, 4, 4}, 0, 20, [4]int{})
	filterLuma8EdgeSSE2(nil, -1, 0, 0, [4]uint8{1, 2, 3, 4}, 20, 0, [4]int{})
	filterLuma8EdgeSSE2(nil, -1, 0, 0, [4]uint8{}, 20, 20, [4]int{})
}

func TestAutoLuma8EdgeSelectsSSE2(t *testing.T) {
	if nativeCPU.sse2 && kernels.backends.FilterLumaEdge != SSE2 {
		t.Fatalf("Auto亮度整边后端=%s，期望SSE2", kernels.backends.FilterLumaEdge)
	}
}
