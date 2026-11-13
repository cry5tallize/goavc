package motion

import "testing"

func TestDistanceScaleFactor_SignedGeometry(t *testing.T) {
	for _, c := range [][4]int32{{2, 0, 8, 64}, {4, 0, 8, 128}, {6, 0, 8, 192}, {-2, 0, 8, -64}, {4, 8, 0, 128}, {4, 0, 0, 256}, {1000, 0, 1, 1023}} {
		if got := DistanceScaleFactor(c[0], c[1], c[2]); got != c[3] {
			t.Fatalf("%v got %d", c, got)
		}
	}
}
