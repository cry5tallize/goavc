package prediction

import "testing"

func TestIntraModes_AvailabilityAndMPM(t *testing.T) {
	for mode := 0; mode < 9; mode++ {
		if !Intra4x4ModeAvailable(mode, true, true, true) {
			t.Fatal(mode)
		}
		if got := Intra4x4ModeAvailable(mode, false, false, false); got != (mode == 2) {
			t.Fatal(mode)
		}
	}
	for _, mode := range []int{4, 5, 6} {
		if Intra4x4ModeAvailable(mode, true, true, false) {
			t.Fatal("missing top-left", mode)
		}
	}
	for _, tc := range [][3]int{{-1, 0, 2}, {8, -1, 2}, {0, 8, 0}, {7, 4, 4}, {2, 1, 1}} {
		if got := MostProbableIntra4x4(tc[0], tc[1]); got != tc[2] {
			t.Fatal(tc, got)
		}
	}
}
