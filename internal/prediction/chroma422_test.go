package prediction

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestChroma422_DCRegions(t *testing.T) {
	s := IntraBlockSamples{
		Top:  []byte{12, 12, 12, 12, 36, 36, 36, 36},
		Left: []byte{52, 52, 52, 52, 92, 92, 92, 92, 132, 132, 132, 132, 172, 172, 172, 172},
	}
	for _, c := range []struct {
		name      string
		top, left bool
		want      [4][2]byte
	}{
		{"neither", false, false, [4][2]byte{{128, 128}, {128, 128}, {128, 128}, {128, 128}}},
		{"both", true, true, [4][2]byte{{32, 36}, {92, 64}, {132, 84}, {172, 104}}},
		{"top", true, false, [4][2]byte{{12, 36}, {12, 36}, {12, 36}, {12, 36}}},
		{"left", false, true, [4][2]byte{{52, 52}, {92, 92}, {132, 132}, {172, 172}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var dst [16 * 11]byte
			for i := range dst {
				dst[i] = 255
			}
			if err := Chroma422(dst[:], 11, 0, s, c.top, c.left); err != nil {
				t.Fatal(err)
			}
			for y := range 16 {
				for x := range 11 {
					want := byte(255)
					if x < 8 {
						want = c.want[y/4][x/4]
					}
					if got := dst[y*11+x]; got != want {
						t.Fatalf("(%d,%d): got %d want %d", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestChroma422_DirectionalAndPlane(t *testing.T) {
	var top [8]byte
	var left [16]byte
	for x := range top {
		top[x] = byte(126 + 4*x)
	}
	for y := range left {
		left[y] = byte(124 + 2*y)
	}
	s := IntraBlockSamples{Top: top[:], Left: left[:], TopLeft: 122}
	for _, mode := range []int{1, 2, 3} {
		var dst [16 * 9]byte
		if err := Chroma422(dst[:], 9, mode, s, true, true); err != nil {
			t.Fatal(err)
		}
		for y := range 16 {
			for x := range 9 {
				want := byte(0)
				if x < 8 {
					switch mode {
					case 1:
						want = left[y]
					case 2:
						want = top[x]
					case 3:
						// 独立线性场 f(x,y)=128+4x+2y；H=240,V=816，b=128,c=64。
						want = byte(128 + 4*x + 2*y)
					}
				}
				if got := dst[y*9+x]; got != want {
					t.Fatalf("mode=%d (%d,%d): got %d want %d", mode, x, y, got, want)
				}
			}
		}
	}
}

func TestChroma422_PlaneClippingAndHighExtremes(t *testing.T) {
	for _, c := range []struct {
		edge, corner byte
		first, last  byte
	}{{0, 255, 86, 0}, {255, 0, 169, 255}} {
		var top [8]byte
		var left [16]byte
		for i := range top {
			top[i] = c.edge
		}
		for i := range left {
			left[i] = c.edge
		}
		var dst [128]byte
		if err := Chroma422(dst[:], 8, 3, IntraBlockSamples{Top: top[:], Left: left[:], TopLeft: c.corner}, true, true); err != nil {
			t.Fatal(err)
		}
		// 解析斜率 b=±542,c=±159；右下角超出 Clip1C 两端。
		if dst[0] != c.first || dst[127] != c.last {
			t.Fatalf("edge=%d: endpoints %d/%d want %d/%d", c.edge, dst[0], dst[127], c.first, c.last)
		}
	}
	for _, c := range []struct {
		edge, corner uint16
		first, last  uint16
	}{{0, 1023, 344, 0}, {1023, 0, 679, 1023}} {
		var top [8]uint16
		var left [16]uint16
		for i := range top {
			top[i] = c.edge
		}
		for i := range left {
			left[i] = c.edge
		}
		s := IntraBlockSamples16{Top: top[:], Left: left[:], TopLeft: c.corner}
		var dst [16 * 11]uint16
		if err := Chroma422High(dst[:], 11, 3, s, true, true, 10); err != nil {
			t.Fatal(err)
		}
		// 10-bit：b=±2174,c=±639；不能先转换到 byte 或裁剪负斜率。
		if dst[0] != c.first || dst[15*11+7] != c.last {
			t.Fatalf("high edge=%d: endpoints %d/%d want %d/%d", c.edge, dst[0], dst[15*11+7], c.first, c.last)
		}
		for _, mode := range []int{0, 1, 2} {
			if err := Chroma422High(dst[:], 11, mode, s, true, true, 10); err != nil {
				t.Fatal(err)
			}
			for y := range 16 {
				for x := range 8 {
					if dst[y*11+x] != c.edge {
						t.Fatalf("high mode=%d (%d,%d): got %d want %d", mode, x, y, dst[y*11+x], c.edge)
					}
				}
			}
		}
		if err := Chroma422High(dst[:], 11, 0, s, false, false, 10); err != nil {
			t.Fatal(err)
		}
		if dst[0] != 512 || dst[15*11+7] != 512 {
			t.Fatal("10-bit DC midpoint is not 512")
		}
	}
}

func TestChroma422_InvalidInputs(t *testing.T) {
	var top [8]byte
	var left [16]byte
	s := IntraBlockSamples{Top: top[:], Left: left[:]}
	var dst [128]byte
	for _, c := range []struct {
		mode      int
		top, left bool
	}{{1, true, false}, {2, false, true}, {3, true, false}, {3, false, true}, {4, true, true}} {
		if err := Chroma422(dst[:], 8, c.mode, s, c.top, c.left); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("%+v: got %v want ErrInvalidData", c, err)
		}
	}
	if err := Chroma422(dst[:127], 8, 0, s, true, true); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("short destination: %v", err)
	}
	if err := Chroma422(dst[:], 7, 0, s, true, true); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("short stride: %v", err)
	}
	s.Left = s.Left[:15]
	if err := Chroma422(dst[:], 8, 0, s, true, true); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("short left: %v", err)
	}
}

func TestChroma422_HighDCRegions(t *testing.T) {
	s := IntraBlockSamples16{
		Top:  []uint16{1000, 1000, 1000, 1000, 900, 900, 900, 900},
		Left: []uint16{800, 800, 800, 800, 700, 700, 700, 700, 600, 600, 600, 600, 500, 500, 500, 500},
	}
	var dst [16 * 9]uint16
	if err := Chroma422High(dst[:], 9, 0, s, true, true, 10); err != nil {
		t.Fatal(err)
	}
	want := [4][2]uint16{{900, 900}, {700, 800}, {600, 750}, {500, 700}}
	for y := range 16 {
		for x := range 9 {
			v := uint16(0)
			if x < 8 {
				v = want[y/4][x/4]
			}
			if got := dst[y*9+x]; got != v {
				t.Fatalf("high DC (%d,%d): got %d want %d", x, y, got, v)
			}
		}
	}
}
