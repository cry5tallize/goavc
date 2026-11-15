package picture

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestPicture444_ThreeFullResolutionPlanesAndBorders(t *testing.T) {
	// Spec: 6.2/6.3，非分离444宏块的三个分量均为16×16。
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		pic, err := New(3, 2, format)
		if err != nil {
			t.Fatal(err)
		}
		limit := uint16((1 << format.BitDepth()) - 1)
		for component := range pic.Planes {
			pl := &pic.Planes[component]
			if pl.Width != 48 || pl.Height != 32 || pl.BitDepth != format.BitDepth() || pl.Stride*format.BytesPerSample()%64 != 0 {
				t.Fatalf("%s plane%d geometry=%+v", format, component, pl)
			}
			need := pl.Stride * (pl.Height + 2*pl.Border)
			if format.BitDepth() == 10 {
				if len(pl.Data) != 0 || len(pl.Data16) != need {
					t.Fatal("10-bit必须仅使用uint16存储")
				}
			} else if len(pl.Data16) != 0 || len(pl.Data) != need {
				t.Fatal("8-bit必须仅使用byte存储")
			}
			for y := range pl.Height {
				for x := range pl.Width {
					pl.SetSample(x, y, uint16((component*137+x*11+y*23))&limit)
				}
			}
			// 行访问必须借用原始缓冲，包含Cb/Cr的最右下角而非420/422尺寸。
			if format.BitDepth() == 10 {
				pl.Row16(31)[47] = limit - uint16(component)
			} else {
				pl.Row(31)[47] = byte(limit - uint16(component))
			}
		}
		pic.FillBorders()
		for component := range pic.Planes {
			pl := &pic.Planes[component]
			for y := -pl.Border; y < pl.Height+pl.Border; y++ {
				for x := -pl.Border; x < pl.Width+pl.Border; x++ {
					cx, cy := max(0, min(pl.Width-1, x)), max(0, min(pl.Height-1, y))
					want := uint16(component*137+cx*11+cy*23) & limit
					if cx == 47 && cy == 31 {
						want = limit - uint16(component)
					}
					if got := pl.Sample(x, y); got != want {
						t.Fatalf("%s plane%d (%d,%d)=%d want%d", format, component, x, y, got, want)
					}
				}
			}
		}
		if err := pic.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPicture444_PoolIsolatesEveryFormatAndCodedSize(t *testing.T) {
	pool := NewPool(1)
	formats := []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE}
	var pictures [2][6]*Picture
	for round := range 3 {
		for size := range 2 {
			for index, format := range formats {
				pic, err := pool.Get(size+1, 2, format)
				if err != nil {
					t.Fatal(err)
				}
				if round == 0 {
					pictures[size][index] = pic
				} else if pic != pictures[size][index] {
					t.Fatalf("%s size%d未复用独立桶", format, size)
				}
				for component := range pic.Planes {
					pl := &pic.Planes[component]
					want := uint16(17 + size*61 + index*19 + component*7)
					if format.BitDepth() == 10 {
						want += 700
					}
					if round == 0 {
						pl.SetSample(pl.Width-1, pl.Height-1, want)
					}
					if got := pl.Sample(pl.Width-1, pl.Height-1); got != want {
						t.Fatalf("%s size%d plane%d跨桶污染: %d want%d", format, size, component, got, want)
					}
				}
				if err := pic.Validate(); err != nil {
					t.Fatal(err)
				}
				pic.Release()
				pic.Release()
			}
		}
	}
	gets, puts, allocs := pool.Stats()
	if gets != 36 || puts != 36 || allocs != 12 {
		t.Fatal(gets, puts, allocs)
	}
}

func TestPicture444_SharedResidualKernelsCoverAllPlanes(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV444P, h264.YUV444P10LE} {
		pic, err := New(1, 1, format)
		if err != nil {
			t.Fatal(err)
		}
		limit := uint16((1 << format.BitDepth()) - 1)
		for component := range pic.Planes {
			pl := &pic.Planes[component]
			for y := range 16 {
				for x := range 16 {
					pl.SetSample(x, y, limit/2)
				}
			}
			var four [16]int32
			var eight [64]int32
			for i := range four {
				four[i] = -1 << 31
			}
			for i := range eight {
				eight[i] = 1<<31 - 1
			}
			AddResidual4x4(pl, 12, 12, &four)
			AddResidual8x8(pl, 8, 0, &eight)
			for y := range 16 {
				for x := range 16 {
					want := limit / 2
					if x >= 12 && y >= 12 {
						want = 0
					} else if x >= 8 && y < 8 {
						want = limit
					}
					if got := pl.Sample(x, y); got != want {
						t.Fatalf("%s plane%d (%d,%d)=%d want%d", format, component, x, y, got, want)
					}
				}
			}
		}
	}
}
