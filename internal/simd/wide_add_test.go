package simd

import (
	"math"
	"testing"
)

func TestWideAdd4_AutoParityAndNoAllocation(t *testing.T) {
	values := [4]int64{math.MaxInt64, math.MinInt64, -1, 1}
	add := [4]int64{1, -1, math.MaxInt64, math.MinInt64}
	if got, want := kernels.wideAdd4(values, add), wideAdd4Generic(values, add); got != want {
		t.Fatalf("wideAdd4 mismatch: got %v want %v", got, want)
	}
	if n := testing.AllocsPerRun(100, func() { _ = kernels.wideAdd4(values, add) }); n != 0 {
		t.Fatalf("wideAdd4 allocated: %v", n)
	}
}
