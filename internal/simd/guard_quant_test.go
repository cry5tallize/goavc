package simd

import (
	"fmt"
	"testing"
)

func TestGuardPage_Quantization(t *testing.T) {
	if !guardChild(t) {
		return
	}
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		kernels = backend.table
		t.Run(backend.name, func(t *testing.T) {
			for _, n := range []int{1, 3, 4, 7, 8, 13, 16, 17, 31} {
				t.Run(fmt.Sprint(n), func(t *testing.T) {
					src, _ := guardedRows(t, n, 1, 4)
					dst, _ := guardedRows(t, n, 1, 4)
					a, b := guardInt32(src), guardInt32(dst)
					for i := range a {
						a[i] = int32(i*193 - 1023)
					}
					QuantizeCoefficients(b, a, 13107, 19, 6)
					for i, v := range b {
						if want := QuantizeWithDeadZone(a[i], 13107, 19, 6); v != want {
							t.Fatalf("DC量化lane%d=%d/%d", i, v, want)
						}
					}
					QuantizeCoefficients(a, a, 13107, 19, 6)
					for i, v := range a {
						if v != b[i] {
							t.Fatalf("原地DC量化lane%d=%d/%d", i, v, b[i])
						}
					}
				})
			}
		})
	}
}
