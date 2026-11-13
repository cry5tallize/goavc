package transform

import "testing"

func TestZigzag4x4(t *testing.T) {
	want := [16]int{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	if Zigzag4x4 != want {
		t.Fatalf("Zigzag4x4 = %v, want %v", Zigzag4x4, want)
	}
	seen := [16]bool{}
	for _, v := range Zigzag4x4 {
		if v < 0 || v >= 16 || seen[v] {
			t.Fatalf("Zigzag4x4 is not a permutation: %v", Zigzag4x4)
		}
		seen[v] = true
	}
}
