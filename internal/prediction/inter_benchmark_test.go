package prediction

import (
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
)

func BenchmarkMCLuma_16x16_Generic(b *testing.B) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		for _, scenario := range []string{"interior", "border", "field"} {
			b.Run(format.String()+"/"+scenario, func(b *testing.B) {
				pic, err := picture.New(4, 8, format)
				if err != nil {
					b.Fatal(err)
				}
				defer pic.Release()
				for y := range pic.Height {
					for x := range pic.Width {
						pic.Planes[0].SetSample(x, y, uint16((x*19+y*31)%((1<<format.BitDepth())-1)))
					}
				}
				ref := &pic.Planes[0]
				x, y := 20, 21
				if scenario == "border" {
					x, y = -2, -3
				}
				if scenario == "field" {
					field, err := picture.FieldView(pic, true)
					if err != nil {
						b.Fatal(err)
					}
					ref = &field.Planes[0]
				}
				var dst8 [256]byte
				var dst16 [256]uint16
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if format.BitDepth() == 8 {
						MCBlockLuma(dst8[:], 16, ref, x, y, 16, 16, 2, 2)
					} else {
						MCBlockLumaHigh(dst16[:], 16, ref, x, y, 16, 16, 2, 2)
					}
				}
			})
		}
	}
}
