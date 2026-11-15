package picture

import (
	"math"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestResidualFieldParityAndExtremes(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		for parity := range 2 {
			p, err := New(1, 2, format)
			if err != nil {
				t.Fatal(err)
			}
			pl := &p.Planes[0]
			for y := range 18 {
				for x := range 12 {
					pl.SetSample(x, y, 101)
				}
			}
			field, err := FieldView(p, parity != 0)
			if err != nil {
				t.Fatal(err)
			}
			var r [64]int32
			for i := range r {
				r[i] = int32(i % 4)
			}
			r[0], r[1] = math.MinInt32, math.MaxInt32
			AddResidual8x8(&field.Planes[0], 2, 1, &r)
			limit := uint16(255)
			if format.BitDepth() == 10 {
				limit = 1023
			}
			for y := range 18 {
				for x := range 12 {
					want := uint16(101)
					if y%2 == parity && y/2 >= 1 && y/2 < 9 && x >= 2 && x < 10 {
						i := (y/2-1)*8 + x - 2
						want = uint16(max(0, min(int64(limit), int64(101)+int64(r[i]))))
					}
					if got := pl.Sample(x, y); got != want {
						t.Fatalf("格式%v 场%d (%d,%d)=%d，期望%d", format, parity, x, y, got, want)
					}
				}
			}
			p.Release()
		}
	}
}
