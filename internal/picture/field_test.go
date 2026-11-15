package picture

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

var fieldFormats = [...]h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE}

func TestFieldView_GeometryAliasingAndParity(t *testing.T) {
	for _, format := range fieldFormats {
		t.Run(format.String(), func(t *testing.T) {
			src, err := New(3, 4, format)
			if err != nil {
				t.Fatal(err)
			}
			for c := range src.Planes {
				pl := &src.Planes[c]
				for y := range pl.Height {
					for x := range pl.Width {
						pl.SetSample(x, y, uint16(20+c*7+y%9))
					}
				}
			}
			for parity := range 2 {
				view, err := FieldView(src, parity != 0)
				if err != nil {
					t.Fatal(err)
				}
				if err := view.Validate(); err != nil {
					t.Fatal(err)
				}
				if view.MBWidth != 3 || view.MBHeight != 2 || view.Width != 48 || view.Height != 32 || view.Format != format || view.pool != nil {
					t.Fatalf("wrong field geometry/ownership: %+v", view)
				}
				for c := range view.Planes {
					pl, orig := &view.Planes[c], &src.Planes[c]
					if pl.Width != orig.Width || pl.Height*2 != orig.Height || pl.Stride != 2*orig.Stride || pl.Border*2 != orig.Border || pl.BitDepth != orig.BitDepth {
						t.Fatalf("plane %d geometry changed", c)
					}
					if format.BitDepth() == 10 {
						if len(pl.Data) != 0 || &pl.Row16(0)[0] != &orig.Row16(parity)[0] {
							t.Fatal("10-bit plane does not borrow original samples")
						}
					} else if len(pl.Data16) != 0 || &pl.Row(0)[0] != &orig.Row(parity)[0] {
						t.Fatal("8-bit plane does not borrow original samples")
					}
					for y := range pl.Height {
						for x := range pl.Width {
							other := orig.Sample(x, 2*y+1-parity)
							value := uint16(101 + parity*31 + c*9 + (x+y)%7)
							if pl.BitDepth == 10 {
								value += 513 // 非零低二位不能被8-bit路径截掉。
							}
							pl.SetSample(x, y, value)
							if orig.Sample(x, 2*y+parity) != value || orig.Sample(x, 2*y+1-parity) != other {
								t.Fatalf("plane %d (%d,%d) changed wrong parity", c, x, y)
							}
						}
					}
				}
			}
			if err := src.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFieldView_EntireBorderStaysInOriginalBuffer(t *testing.T) {
	for _, format := range fieldFormats {
		for _, border := range []int{0, 1, 3, DefaultBorder} {
			src, err := New(1, 2, format)
			if err != nil {
				t.Fatal(err)
			}
			for c := range src.Planes {
				orig := &src.Planes[c]
				*orig = allocPlane(orig.Width, orig.Height, border, format.BitDepth())
				for y := -border; y < orig.Height+border; y++ {
					for x := -border; x < orig.Width+border; x++ {
						orig.SetSample(x, y, uint16((x+border+3*(y+border)+c*17)&((1<<format.BitDepth())-1)))
					}
				}
			}
			for parity := range 2 {
				view, err := FieldView(src, parity != 0)
				if err != nil {
					t.Fatal(err)
				}
				if err := view.Validate(); err != nil {
					t.Fatal(err)
				}
				for c := range view.Planes {
					pl, orig := &view.Planes[c], &src.Planes[c]
					for y := -pl.Border; y < pl.Height+pl.Border; y++ {
						for x := -pl.Border; x < pl.Width+pl.Border; x++ {
							if got, want := pl.Sample(x, y), orig.Sample(x, 2*y+parity); got != want {
								t.Fatalf("%s border %d parity %d plane %d (%d,%d): %d want %d", format, border, parity, c, x, y, got, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestFieldView_OwnershipAndAllocation(t *testing.T) {
	pool := NewPool(1)
	for _, format := range fieldFormats {
		src, err := pool.Get(2, 2, format)
		if err != nil {
			t.Fatal(err)
		}
		beforeGets, beforePuts, beforeAllocs := pool.Stats()
		for parity := range 2 {
			view, err := FieldView(src, parity != 0)
			if err != nil {
				t.Fatal(err)
			}
			view.Release()
			view.Release()
			if src.pool != pool {
				t.Fatal("field release detached source pool")
			}
		}
		if gets, puts, allocs := pool.Stats(); gets != beforeGets || puts != beforePuts || allocs != beforeAllocs {
			t.Fatal("field view changed pool stats", gets, puts, allocs)
		}
		if allocs := testing.AllocsPerRun(100, func() {
			view, err := FieldView(src, true)
			if err != nil {
				panic(err)
			}
			view.Planes[0].SetSample(0, 0, 123)
		}); allocs != 0 {
			t.Fatalf("FieldView allocated %g objects", allocs)
		}
		src.Release()
		reused, err := pool.Get(2, 2, format)
		if err != nil {
			t.Fatal(err)
		}
		if reused != src || reused.Format != format || reused.MBHeight != 2 || reused.Planes[0].fieldView || reused.Planes[0].Border != DefaultBorder {
			t.Fatal("field corrupted source pool bucket/layout")
		}
		if err := reused.Validate(); err != nil {
			t.Fatal(err)
		}
		reused.Release()
	}
}

func TestFieldView_RejectsInvalidSourcesWithClassification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Picture)
		want   error
	}{
		{"odd MB height", func(p *Picture) {
			p.MBHeight = 1
			p.Height = 16
			for c := range p.Planes {
				_, p.Planes[c].Height = p.Format.PlaneDimensions(c, p.Width, p.Height)
			}
		}, h264.ErrInvalidData},
		{"coded geometry", func(p *Picture) { p.Height-- }, h264.ErrInvalidData},
		{"missing buffer", func(p *Picture) { p.Planes[1].Data = nil }, h264.ErrInvalidData},
		{"mixed bit depth", func(p *Picture) { p.Planes[0].Data16 = []uint16{1} }, h264.ErrInvalidData},
		{"invalid stride", func(p *Picture) { p.Planes[2].Stride = 1 }, h264.ErrInvalidData},
		{"invalid border", func(p *Picture) { p.Planes[2].Border = -1 }, h264.ErrInvalidData},
		{"unsupported format", func(p *Picture) { p.Format = h264.PixelFormat(255) }, h264.ErrUnsupported},
		{"resource limit", func(p *Picture) { p.MBWidth = h264.MaxFrameWidth/16 + 1 }, h264.ErrResourceLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := New(1, 2, h264.YUV420P)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(src)
			for _, bottom := range []bool{false, true} {
				view, err := FieldView(src, bottom)
				if !errors.Is(err, tc.want) || view.MBWidth != 0 || view.Planes[0].Data != nil {
					t.Fatalf("FieldView=%+v, %v; want zero view and %v", view, err, tc.want)
				}
			}
		})
	}
	if _, err := FieldView(nil, false); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("nil source classification", err)
	}
	src, err := New(1, 4, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	view, err := FieldView(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FieldView(&view, false); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("nested field view classification", err)
	}
}

func TestFieldView_RejectsIndependentBorderWrites(t *testing.T) {
	for _, format := range fieldFormats {
		src, err := New(1, 2, format)
		if err != nil {
			t.Fatal(err)
		}
		src.Planes[0].SetSample(0, 0, 71)
		src.Planes[0].SetSample(0, 1, 83)
		src.FillBorders()
		for parity := range 2 {
			view, err := FieldView(src, parity != 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, fill := range []func(){view.FillBorders, view.Planes[0].FillBorder} {
				func() {
					defer func() {
						if recover() == nil {
							t.Error("borrowed border write did not panic")
						}
					}()
					fill()
				}()
			}
			if src.Planes[0].Sample(0, 0) != 71 || src.Planes[0].Sample(0, 1) != 83 || src.Planes[0].Sample(0, -1) != 71 {
				t.Fatal("borrowed border operation mutated source")
			}
		}
	}
}
