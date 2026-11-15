package simd

import "testing"

func TestMCLuma_Generic_StrideAndPhase(t *testing.T) {
	const (
		w, h   = 13, 11
		stride = 29
	)
	for _, tc := range []struct {
		fx, fy int
	}{
		{0, 0}, {1, 0}, {2, 0}, {3, 0},
		{0, 1}, {0, 2}, {0, 3},
		{1, 1}, {1, 2}, {2, 2}, {3, 3},
	} {
		ref := make([]byte, 32*stride)
		for i := range ref {
			ref[i] = byte((i*37 + 11) & 255)
		}
		for i := range dst {
			dst[i] = 0xa5
		}
		MCLuma(dst[3:], stride, ref[2*stride+3:], stride, w, h, tc.fx, tc.fy, 8)
		for y := range h {
			for x := w; x < stride; x++ {
				if dst[3+y*stride+x] != 0xa5 {
					t.Fatalf("phase %d/%d overwrote padding at %d,%d", tc.fx, tc.fy, x, y)
				}
			}
		}
	}
}

func TestMCChroma_Generic_AllPhasesAndBitDepth(t *testing.T) {
	const stride = 23
	for _, bits := range []int{8, 10} {
		for fy := range 8 {
			for fx := range 8 {
				ref := make([]uint16, 16*stride)
				for i := range ref {
					ref[i] = uint16((i*29 + fx*7 + fy*13) & ((1 << bits) - 1))
				}
				dst := make([]uint16, 7*stride)
				for i := range dst {
					dst[i] = 0xffff
				}
				MCChroma(dst[2:], stride, ref[3*stride+2:], stride, 9, 7, fx, fy)
                for y := range 7 {
                    for x := range 9 {
                        v := dst[2+y*stride+x]
                        i := y*stride + x
                        a := (8-fx)*(8-fy)
                        b := fx * (8 - fy)
                        c := (8-fx)*fy
                        d := fx * fy
                        base := 3*stride + 2 + i
                        want := uint16((a*int(ref[base]) + b*int(ref[base+1]) + c*int(ref[base+stride]) + d*int(ref[base+stride+1]) + 32) >> 6)
                        if int(v) > (1<<bits)-1 {
                            t.Fatalf("bits=%d phase=%d/%d out of range: %d", bits, fx, fy, v)
                        }
                        if v != want {
                            t.Fatalf("bits=%d phase=%d/%d at %d,%d: got %d want %d", bits, fx, fy, x, y, v, want)
                        }
                    }
                    for x := 9; x < stride-2; x++ {
                        if dst[2+y*stride+x] != 0xffff {
                            t.Fatalf("bits=%d phase=%d/%d overwrote padding", bits, fx, fy)
                        }
                    }
                }
			}
		}
	}
}
