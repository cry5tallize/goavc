package prediction

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func fill(samples []byte, v byte) {
	for i := range samples {
		samples[i] = v
	}
}

func TestIntraBlock_ConstantAndDC(t *testing.T) {
	const c = 91
	for _, n := range []int{8, 16} {
		top := make([]byte, n)
		left := make([]byte, n)
		fill(top, c)
		fill(left, c)
		s := IntraBlockSamples{Top: top, Left: left, TopLeft: c}
		dst := make([]byte, n*n)
		for mode := 0; mode <= 3; mode++ {
			var err error
			if n == 16 {
				err = Intra16x16(dst, n, mode, s, true, true)
			} else {
				err = Chroma8x8(dst, n, mode, s, true, true)
			}
			if err != nil {
				t.Fatalf("n=%d mode=%d: %v", n, mode, err)
			}
			for i, v := range dst {
				if v != c {
					t.Fatalf("n=%d mode=%d dst[%d]=%d", n, mode, i, v)
				}
			}
		}
	}
}

func TestIntra16x16_DCCombinations(t *testing.T) {
	top := make([]byte, 16)
	left := make([]byte, 16)
	fill(top, 10)
	fill(left, 20)
	s := IntraBlockSamples{Top: top, Left: left}
	dst := make([]byte, 256)

	if err := Intra16x16(dst, 16, Intra16x16DC, s, true, true); err != nil {
		t.Fatal(err)
	}
	want := byte((160 + 320 + 16) >> 5)
	if dst[0] != want {
		t.Fatalf("both dc = %d, want %d", dst[0], want)
	}
	if err := Intra16x16(dst, 16, Intra16x16DC, s, true, false); err != nil {
		t.Fatal(err)
	}
	if want := byte((160 + 8) >> 4); dst[0] != want {
		t.Fatalf("top dc = %d, want %d", dst[0], want)
	}
	if err := Intra16x16(dst, 16, Intra16x16DC, s, false, true); err != nil {
		t.Fatal(err)
	}
	if want := byte((320 + 8) >> 4); dst[0] != want {
		t.Fatalf("left dc = %d, want %d", dst[0], want)
	}
	if err := Intra16x16(dst, 16, Intra16x16DC, s, false, false); err != nil {
		t.Fatal(err)
	}
	if dst[0] != 128 {
		t.Fatalf("no-neighbor dc = %d, want 128", dst[0])
	}
}

func TestChroma8x8_DC(t *testing.T) {
	top := make([]byte, 8)
	left := make([]byte, 8)
	fill(top, 10)
	fill(left, 20)
	s := IntraBlockSamples{Top: top, Left: left}
	dst := make([]byte, 64)
	if err := Chroma8x8(dst, 8, 0, s, true, true); err != nil {
		t.Fatal(err)
	}
	// 8x8 DC 按四个象限：左上 (top0+left0+4)>>3=15、右上 (top1+2)>>2=10、
	// 左下 (left1+2)>>2=20、右下 (top1+left1+4)>>3=15。
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			want := byte(15)
			if y < 4 && x >= 4 {
				want = 10
			} else if y >= 4 && x < 4 {
				want = 20
			}
			if got := dst[y*8+x]; got != want {
				t.Fatalf("dc[%d][%d] = %d, want %d", y, x, got, want)
			}
		}
	}
}

func TestIntraBlock_PlaneRequiresBoth(t *testing.T) {
	top := make([]byte, 16)
	left := make([]byte, 16)
	s := IntraBlockSamples{Top: top, Left: left}
	dst := make([]byte, 256)
	if err := Intra16x16(dst, 16, Intra16x16Plane, s, true, false); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("err = %v, want ErrInvalidData", err)
	}
}
