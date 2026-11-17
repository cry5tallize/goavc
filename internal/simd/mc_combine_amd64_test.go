//go:build amd64 && !purego

package simd

import (
	"fmt"
	"testing"
)

func TestGuardPage_MCCombine8SSE2(t *testing.T) {
	if !nativeCPU.sse2 {
		t.Skip("当前CPU没有SSE2")
	}
	if !guardChild(t) {
		return
	}
	for _, w := range []int{1, 3, 4, 7, 8, 15, 16, 17} {
		for _, second := range []bool{false, true} {
			for _, avg := range []bool{false, true} {
				t.Run(fmt.Sprintf("w%d/b%t/avg%t", w, second, avg), func(t *testing.T) {
					a, as := guardedRows(t, w, 5, 1)
					b, bs := guardedRows(t, w, 5, 1)
					dst, ds := guardedRows(t, w, 5, 1)
					var want [5][17]byte
					for y := range 5 {
						for x := range w {
							a[y*as+x], b[y*bs+x], dst[y*ds+x] = byte(x*71+y), byte(255-x*7-y), byte(x+y*17)
							v := int(a[y*as+x])
							if second {
								v = (v + int(b[y*bs+x]) + 1) >> 1
							}
							if avg {
								v = (v + int(dst[y*ds+x]) + 1) >> 1
							}
							want[y][x] = byte(v)
						}
					}
					if !second {
						b = nil
					}
					mcCombine8SSE2(dst, ds, a, as, b, bs, w, 5, avg)
					for y := range 5 {
						for x := range w {
							if dst[y*ds+x] != want[y][x] {
								t.Fatal("两级舍入或保护页结果不符")
							}
						}
					}
				})
			}
		}
	}
}
