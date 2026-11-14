package picture

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestNew_GeometryAndAlignment(t *testing.T) {
	pic, err := New(4, 3, h264.YUV420P)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := pic.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if pic.Width != 64 || pic.Height != 48 {
		t.Fatalf("coded size = %dx%d, want 64x48", pic.Width, pic.Height)
	}
	for i, want := range [][2]int{{64, 48}, {32, 24}, {32, 24}} {
		pl := pic.Planes[i]
		if pl.Width != want[0] || pl.Height != want[1] {
			t.Errorf("plane %d = %dx%d, want %dx%d", i, pl.Width, pl.Height, want[0], want[1])
		}
		if pl.Stride%64 != 0 {
			t.Errorf("plane %d stride %d not 64-aligned", i, pl.Stride)
		}
		if pl.Stride < pl.Width+2*pl.Border {
			t.Errorf("plane %d stride %d < width+2*border %d", i, pl.Stride, pl.Width+2*pl.Border)
		}
	}
}

func TestNew_RejectsInvalidDimensions(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		want error
	}{
		{"zero width", 0, 4, h264.ErrInvalidData},
		{"negative height", 4, -1, h264.ErrInvalidData},
		{"over max width", h264.MaxFrameWidth/16 + 1, 4, h264.ErrResourceLimit},
		{"over max height", 4, h264.MaxFrameHeight/16 + 1, h264.ErrResourceLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.w, tt.h, h264.YUV420P)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPlane_ImageCoordinatesAndBorder(t *testing.T) {
	pic, err := New(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	luma := &pic.Planes[0]
	for y := 0; y < luma.Height; y++ {
		for x := 0; x < luma.Width; x++ {
			luma.SetPix(x, y, byte(x+y*17))
		}
	}
	for y := 0; y < luma.Height; y++ {
		for x := 0; x < luma.Width; x++ {
			if got, want := luma.Pix(x, y), byte(x+y*17); got != want {
				t.Fatalf("Pix(%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
	row := luma.Row(3)
	if len(row) != luma.Width {
		t.Fatalf("Row length = %d, want %d", len(row), luma.Width)
	}
	if row[2] != byte(2+3*17) {
		t.Fatalf("Row(3)[2] = %d", row[2])
	}
}

func TestFillBorder_EdgeReplication(t *testing.T) {
	pic, err := New(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	for pi := range pic.Planes {
		pl := &pic.Planes[pi]
		for y := 0; y < pl.Height; y++ {
			for x := 0; x < pl.Width; x++ {
				pl.SetPix(x, y, byte(10+x*3+y*5))
			}
		}
	}
	pic.FillBorders()

	for pi := range pic.Planes {
		pl := &pic.Planes[pi]
		b := pl.Border
		// 四角：等于最近的有效角像素。
		corners := []struct {
			x, y   int
			want   byte
			source [2]int
		}{
			{-1, -1, pl.Pix(0, 0), [2]int{0, 0}},
			{pl.Width, -1, pl.Pix(pl.Width-1, 0), [2]int{pl.Width - 1, 0}},
			{-1, pl.Height, pl.Pix(0, pl.Height-1), [2]int{0, pl.Height - 1}},
			{pl.Width, pl.Height, pl.Pix(pl.Width-1, pl.Height-1), [2]int{pl.Width - 1, pl.Height - 1}},
			{-b, -b, pl.Pix(0, 0), [2]int{0, 0}},
		}
		for _, c := range corners {
			if got := pl.Pix(c.x, c.y); got != c.want {
				t.Errorf("plane %d Pix(%d,%d) = %d, want %d", pi, c.x, c.y, got, c.want)
			}
		}
		// 左右边界每一行等于该行边缘像素。
		for y := 0; y < pl.Height; y++ {
			if got, want := pl.Pix(-b, y), pl.Pix(0, y); got != want {
				t.Errorf("plane %d left border y=%d: %d != %d", pi, y, got, want)
			}
			if got, want := pl.Pix(pl.Width+b-1, y), pl.Pix(pl.Width-1, y); got != want {
				t.Errorf("plane %d right border y=%d: %d != %d", pi, y, got, want)
			}
		}
	}
}
