//go:build amd64 && !purego

package simd

import (
	"fmt"
	"testing"
)

func TestGuardPage_AverageBlockSSE2(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU没有SSE2")
	}
	if !guardChild(t) {
		return
	}
	for _, w := range []int{1, 3, 4, 7, 8, 15, 16, 17, 31} {
		for _, unit := range []int{1, 2} {
			t.Run(fmt.Sprintf("w%d/u%d", w, unit), func(t *testing.T) {
				a, ss := guardedRows(t, w, 5, unit)
				b, _ := guardedRows(t, w, 5, unit)
				dst, ds := guardedRows(t, w, 5, unit)
				if unit == 1 {
					for y := range 5 {
						for x := range w {
							a[y*ss+x], b[y*ss+x] = byte(x*71+y), byte(255-x*7-y)
						}
					}
					averageBlock8SSE2(dst, ds, a, b, ss, w, 5)
					for y := range 5 {
						for x := range w {
							if dst[y*ds+x] != byte((int(a[y*ss+x])+int(b[y*ss+x])+1)>>1) {
								t.Fatal("8-bit舍入不一致")
							}
						}
					}
				} else {
					u, v, out := guardUint16(a), guardUint16(b), guardUint16(dst)
					for y := range 5 {
						for x := range w {
							u[y*ss+x], v[y*ss+x] = uint16(x*131+y), uint16(65535-x*73-y)
						}
					}
					averageBlock16SSE2(out, ds, u, v, ss, w, 5)
					for y := range 5 {
						for x := range w {
							if out[y*ds+x] != uint16((int(u[y*ss+x])+int(v[y*ss+x])+1)>>1) {
								t.Fatal("16-bit舍入不一致")
							}
						}
					}
				}
			})
		}
	}
}
