package prediction

import (
	"errors"
	h264 "github.com/goavc/h264"
	"testing"
)

func TestIntra8x8_ConstantAndAvailability(t *testing.T) {
	s := Intra8x8Samples{TopLeft: 77, TopLeftAvailable: true}
	for i := range s.Top {
		s.Top[i] = 77
	}
	for i := range s.Left {
		s.Left[i] = 77
	}
	for mode := 0; mode < 9; mode++ {
		var out [64]byte
		if err := Intra8x8(out[:], 8, mode, s, true, true); err != nil {
			t.Fatal(err)
		}
		for _, v := range out {
			if v != 77 {
				t.Fatal(mode, out)
			}
		}
	}
	var out [64]byte
	if err := Intra8x8(out[:], 8, 2, s, false, false); err != nil || out[0] != 128 {
		t.Fatal(err, out)
	}
	s.TopLeftAvailable = false
	if err := Intra8x8(out[:], 8, 4, s, true, true); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("diagonal mode accepted missing top-left", err)
	}
}

func TestIntra8x8_FilteredDirectionalAnchors(t *testing.T) {
	s := Intra8x8Samples{TopLeft: 70, TopLeftAvailable: true}
	for i := range s.Top {
		s.Top[i] = byte((i + 1) * 10)
	}
	for i := range s.Left {
		s.Left[i] = byte(90 + i*10)
	}
	for _, c := range []struct {
		mode          int
		first, second byte
	}{{0, 28, 20}, {1, 88, 88}, {2, 86, 86}, {3, 25, 30}, {4, 59, 34}, {5, 44, 24}, {6, 74, 59}, {7, 24, 25}, {8, 94, 100}} {
		var out [64]byte
		if err := Intra8x8(out[:], 8, c.mode, s, true, true); err != nil {
			t.Fatal(err)
		}
		if out[0] != c.first || out[1] != c.second {
			t.Fatalf("mode %d: %d,%d want %d,%d", c.mode, out[0], out[1], c.first, c.second)
		}
	}
}
