package prediction

import (
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
)

func TestMCAverage_AllPhasesAndBounds(t *testing.T) {
	pic, err := picture.New(2, 2, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	defer pic.Release()
	for c := range 3 {
		p := &pic.Planes[c]
		for y := range p.Height {
			for x := range p.Width {
				p.SetPix(x, y, byte(x*71+y*37))
			}
		}
	}
	for _, chroma := range []bool{false, true} {
		p, phases := &pic.Planes[0], 4
		if chroma {
			p, phases = &pic.Planes[1], 8
		}
		for fy := range phases {
			for fx := range phases {
				for _, xy := range [][2]int{{-2, -3}, {4, 5}, {p.Width - 2, p.Height - 2}} {
					var got, want, put [23 * 16]byte
					for i := range got {
						got[i] = byte(i * 17)
						want[i] = got[i]
					}
					if chroma {
						MCBlockChromaFormat(put[1:], 23, p, xy[0], xy[1], 7, 5, fx-8, fy-8, 2, 2)
						MCBlockChromaFormatAverage(got[1:], 23, p, xy[0], xy[1], 7, 5, fx-8, fy-8, 2, 2)
					} else {
						MCBlockLuma(put[1:], 23, p, xy[0], xy[1], 7, 5, fx-4, fy-4)
						MCBlockLumaAverage(got[1:], 23, p, xy[0], xy[1], 7, 5, fx-4, fy-4)
					}
					for y := range 5 {
						for x := range 7 {
							i := 1 + y*23 + x
							want[i] = byte((int(want[i]) + int(put[i]) + 1) >> 1)
						}
					}
					if !slices.Equal(got[:], want[:]) {
						t.Fatalf("chroma%t phase%d,%d xy%v不符或写入矩形外", chroma, fx, fy, xy)
					}
				}
			}
		}
	}
	if n := testing.AllocsPerRun(100, func() {
		var data [256]byte
		MCBlockLumaAverage(data[:], 16, &pic.Planes[0], 4, 4, 16, 16, 1, 2)
		MCBlockChromaFormatAverage(data[:], 16, &pic.Planes[1], 0, 0, 8, 8, -3, -5, 2, 2)
	}); n != 0 {
		t.Fatalf("融合MC局部缓冲分配%g次", n)
	}
}
