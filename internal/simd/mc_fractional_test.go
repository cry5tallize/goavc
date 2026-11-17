package simd

import (
	"bytes"
	"testing"
)

func TestMCFractional_ScalarParityAllPhases(t *testing.T) {
	previous := kernels
	defer func() { kernels = previous }()
	for _, w := range []int{4, 8, 12, 16} {
		for _, chroma := range []bool{false, true} {
			phases := 4
			if chroma {
				phases = 8
			}
			for fy := range phases {
				for fx := range phases {
					h, rw, rh, rs, ds := 7, w, 7, 29, 23
					if chroma {
						if fx != 0 {
							rw++
						}
						if fy != 0 {
							rh++
						}
					} else {
						if fx != 0 {
							rw += 5
						}
						if fy != 0 {
							rh += 5
						}
					}
					src := make([]byte, (rh-1)*rs+rw+1)
					for i := range src {
						src[i] = byte(i*71 + i/17*13)
					}
					for _, avg := range []bool{false, true} {
						got := make([]byte, (h-1)*ds+w+1)
						for i := range got {
							got[i] = byte(i*37 + 7)
						}
						want := bytes.Clone(got)
						generic := previous
						generic.backends.MCLumaFractional8, generic.backends.MCChroma8 = Generic, Generic
						kernels = generic
						if chroma {
							mcChromaMode(want[1:], ds, src[1:], rs, w, h, fx, fy, avg)
						} else {
							mcLumaMode(want[1:], ds, src[1:], rs, w, h, fx, fy, 8, avg)
						}
						kernels = previous
						if chroma {
							mcChromaMode(got[1:], ds, src[1:], rs, w, h, fx, fy, avg)
						} else {
							mcLumaMode(got[1:], ds, src[1:], rs, w, h, fx, fy, 8, avg)
						}
						if !bytes.Equal(got, want) {
							t.Fatalf("w%d chroma%t avg%t phase%d,%d不符或越界写", w, chroma, avg, fx, fy)
						}
					}
				}
			}
		}
	}
}
