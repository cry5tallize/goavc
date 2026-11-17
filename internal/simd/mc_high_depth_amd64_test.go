//go:build amd64 && !purego

package simd

import "testing"

func TestGuardPage_MCHighDepth(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU没有SSE2")
	}
	if !guardChild(t) {
		return
	}
	for _, w := range []int{4, 8, 12, 16} {
		for fy := range 4 {
			for fx := range 4 {
				rw, rh := w, 7
				if fx != 0 {
					rw += 5
				}
				if fy != 0 {
					rh += 5
				}
				s, rs := guardedRows(t, rw, rh, 2)
				d, ds := guardedRows(t, w, 7, 2)
				src, dst := guardUint16(s), guardUint16(d)
				for y := range rh {
					for x := range rw {
						src[y*rs+x] = uint16((y*71 + x*137) % 1024)
					}
				}
				MCLuma(dst, ds, src, rs, w, 7, fx, fy, 10)
				var want [7 * 16]uint16
				previous := kernels.backends.MCLumaFractional16
				kernels.backends.MCLumaFractional16 = Generic
				MCLuma(want[:], 16, src, rs, w, 7, fx, fy, 10)
				kernels.backends.MCLumaFractional16 = previous
				for y := range 7 {
					for x := range w {
						if dst[y*ds+x] != want[y*16+x] {
							t.Fatal("保护页MC像素不符")
						}
					}
				}
			}
		}
	}
}

func TestGuardPage_MCChromaHighDepth(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU没有SSE2")
	}
	if !guardChild(t) {
		return
	}
	for _, w := range []int{4, 8, 12, 16} {
		for fy := range 8 {
			for fx := range 8 {
				rw, rh := w, 7
				if fx != 0 {
					rw++
				}
				if fy != 0 {
					rh++
				}
				s, rs := guardedRows(t, rw, rh, 2)
				d, ds := guardedRows(t, w, 7, 2)
				src, dst := guardUint16(s), guardUint16(d)
				for y := range rh {
					for x := range rw {
						src[y*rs+x] = uint16((y*71 + x*137) % 1024)
					}
				}
				MCChroma(dst, ds, src, rs, w, 7, fx, fy)
			}
		}
	}
}
