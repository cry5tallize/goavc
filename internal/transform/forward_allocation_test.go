package transform

import "testing"

func TestForward_InPlaceAndLocalAllocation(t *testing.T) {
	var src [16]int32
	for i := range src {
		src[i] = int32(i*31 - 237)
	}
	var want [16]int32
	Forward4x4(&want, &src)
	if n := testing.AllocsPerRun(100, func() {
		local := src
		Forward4x4(&local, &local)
		if local != want {
			panic("in-place")
		}
	}); n != 0 {
		t.Fatal(n)
	}
}
