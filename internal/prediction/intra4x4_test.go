package prediction

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestIntra4x4_ConstantNeighbors(t *testing.T) {
	const c = 77
	var s Intra4x4Samples
	fill(s.Top[:], c)
	fill(s.TopRight[:], c)
	fill(s.Left[:], c)
	s.TopLeft = c
	for mode := 0; mode <= 8; mode++ {
		dst := make([]byte, 4*4)
		if err := Intra4x4(dst, 4, mode, s, true, true); err != nil {
			t.Fatalf("mode %d: %v", mode, err)
		}
		for i, v := range dst {
			if v != c {
				t.Fatalf("mode %d dst[%d] = %d, want %d", mode, i, v, c)
			}
		}
	}
}

func TestIntra4x4_VerticalHorizontalDC(t *testing.T) {
	s := Intra4x4Samples{
		Top:      [4]byte{10, 20, 30, 40},
		TopRight: [4]byte{50, 60, 70, 80},
		Left:     [4]byte{1, 2, 3, 4},
		TopLeft:  100,
	}
	dst := make([]byte, 16)
	if err := Intra4x4(dst, 4, Intra4x4Vertical, s, true, true); err != nil {
		t.Fatal(err)
	}
	for row := 0; row < 4; row++ {
		want := []byte{10, 20, 30, 40}
		for x := 0; x < 4; x++ {
			if dst[row*4+x] != want[x] {
				t.Fatalf("vertical row %d = %v", row, dst[row*4:row*4+4])
			}
		}
	}
	if err := Intra4x4(dst, 4, Intra4x4Horizontal, s, true, true); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if got, want := dst[y*4+x], s.Left[y]; got != want {
				t.Fatalf("horizontal [%d][%d] = %d, want %d", y, x, got, want)
			}
		}
	}
	if err := Intra4x4(dst, 4, Intra4x4DC, s, true, true); err != nil {
		t.Fatal(err)
	}
	wantDC := byte((10 + 20 + 30 + 40 + 1 + 2 + 3 + 4 + 4) >> 3)
	for _, v := range dst {
		if v != wantDC {
			t.Fatalf("dc = %d, want %d", v, wantDC)
		}
	}
	// 只有上方可用：DC = (100+2)>>2 = 25
	if err := Intra4x4(dst, 4, Intra4x4DC, s, true, false); err != nil {
		t.Fatal(err)
	}
	if dst[0] != 25 {
		t.Fatalf("top-only dc = %d, want 25", dst[0])
	}
	// 两侧都不可用：128
	if err := Intra4x4(dst, 4, Intra4x4DC, s, false, false); err != nil {
		t.Fatal(err)
	}
	if dst[0] != 128 {
		t.Fatalf("no-neighbor dc = %d, want 128", dst[0])
	}
}

func TestIntra4x4_DiagonalLeft(t *testing.T) {
	s := Intra4x4Samples{
		Top:      [4]byte{1, 2, 3, 4},
		TopRight: [4]byte{5, 6, 7, 8},
	}
	dst := make([]byte, 16)
	if err := Intra4x4(dst, 4, Intra4x4DiagonalLeft, s, true, false); err != nil {
		t.Fatal(err)
	}
	want := [16]byte{
		2, 3, 4, 5,
		3, 4, 5, 6,
		4, 5, 6, 7,
		5, 6, 7, 8,
	}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst = %v, want %v", dst, want)
		}
	}
}

func TestIntra4x4_RequiresNeighbors(t *testing.T) {
	var s Intra4x4Samples
	dst := make([]byte, 16)
	cases := []struct {
		mode      int
		top, left bool
	}{
		{Intra4x4Vertical, false, true},
		{Intra4x4Horizontal, true, false},
		{Intra4x4DiagonalRight, true, false},
		{Intra4x4VerticalRight, false, true},
		{Intra4x4HorizontalDown, true, false},
		{Intra4x4VerticalLeft, false, true},
		{Intra4x4HorizontalUp, true, false},
	}
	for _, c := range cases {
		if err := Intra4x4(dst, 4, c.mode, s, c.top, c.left); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("mode %d err = %v, want ErrInvalidData", c.mode, err)
		}
	}
}
