package picture

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestPicture422_GeometrySamplesAndPoolIsolation(t *testing.T) {
	for _, tc := range []struct {
		format h264.PixelFormat
		depth  int
		limit  uint16
	}{{h264.YUV422P, 8, 255}, {h264.YUV422P10LE, 10, 1023}} {
		pool := NewPool(1)
		old, err := pool.Get(3, 2, h264.YUV420P)
		if err != nil {
			t.Fatal(err)
		}
		old.Release()
		pic, err := pool.Get(3, 2, tc.format)
		if err != nil {
			t.Fatal(err)
		}
		if pic.Format != tc.format || pic.Width != 48 || pic.Height != 32 {
			t.Fatalf("coded layout: %+v", pic)
		}
		for comp, dims := range [][2]int{{48, 32}, {24, 32}, {24, 32}} {
			pl := &pic.Planes[comp]
			if pl.Width != dims[0] || pl.Height != dims[1] || pl.BitDepth != tc.depth {
				t.Fatalf("%s plane%d=%dx%d depth%d", tc.format, comp, pl.Width, pl.Height, pl.BitDepth)
			}
			pl.SetSample(0, 0, 0)
			pl.SetSample(pl.Width-1, pl.Height-1, tc.limit)
		}
		pic.FillBorders()
		for comp := range pic.Planes {
			pl := &pic.Planes[comp]
			if pl.Sample(-pl.Border, -pl.Border) != 0 || pl.Sample(pl.Width+pl.Border-1, pl.Height+pl.Border-1) != tc.limit {
				t.Fatalf("%s plane%d border loses samples", tc.format, comp)
			}
		}
		if err := pic.Validate(); err != nil {
			t.Fatal(err)
		}
		pic.Release()
		reuse, err := pool.Get(3, 2, tc.format)
		if err != nil {
			t.Fatal(err)
		}
		if reuse.Planes[1].Sample(23, 31) != tc.limit {
			t.Fatal("pool changed 422 lower chroma rows")
		}
		reuse.Release()
	}
}
