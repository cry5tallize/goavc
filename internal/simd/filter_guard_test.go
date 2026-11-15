package simd

import "testing"

func TestFilterGuardMinimalLumaAndChroma(t *testing.T) {
	if !guardChild(t) {
		return
	}
	for _, width := range []int{4, 6, 8} {
		data, stride := guardedRows(t, width, 4, 1)
		for line := range 4 {
			for i := range width {
				data[line*stride+i] = byte(100 + i)
			}
		}
		if width == 8 {
			FilterLuma8(data, 4, stride, 1, 4, 80, 40, 4, 4, 8)
			FilterLuma8(data, 4, stride, 1, 1, 80, 40, 4, 4, 8)
		} else if width == 6 {
			FilterLuma8(data, 3, stride, 1, 1, 80, 40, 4, 4, 8)
		} else {
			FilterChroma8(data, 2, stride, 1, 4, 80, 40, 5, 4, 8)
			FilterChroma8(data, 2, stride, 1, 1, 80, 40, 5, 4, 8)
		}
	}
}
