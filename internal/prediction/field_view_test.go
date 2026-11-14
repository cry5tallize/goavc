package prediction_test

import (
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/internal/prediction"
)

func TestFieldView_MCFieldEdgesAllFormats(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		t.Run(format.String(), func(t *testing.T) {
			src, err := picture.New(2, 2, format)
			if err != nil {
				t.Fatal(err)
			}
			for c := range src.Planes {
				pl := &src.Planes[c]
				for y := range pl.Height {
					for x := range pl.Width {
						value := uint16(17 + 71*(y%2) + (x*3+y/2*5+c*7)%31)
						if format.BitDepth() == 10 {
							value += 513
						}
						pl.SetSample(x, y, value)
					}
				}
			}
			// 故意不FillBorders。MC必须按场Height/Width钳位，不能读帧边界、
			// 未填充border或另一场；另建紧凑场作为共享插值内核的布局参照。
			for parity := range 2 {
				view, err := picture.FieldView(src, parity != 0)
				if err != nil {
					t.Fatal(err)
				}
				ref, err := picture.New(2, 1, format)
				if err != nil {
					t.Fatal(err)
				}
				for c := range view.Planes {
					pl, rp := &view.Planes[c], &ref.Planes[c]
					for y := range rp.Height {
						for x := range rp.Width {
							rp.SetSample(x, y, src.Planes[c].Sample(x, 2*y+parity))
						}
					}
					subW, subH := format.ChromaSubsampling()
					for _, pos := range [][2]int{{-1, -1}, {0, 0}, {pl.Width - 1, 0}, {0, pl.Height - 1}, {pl.Width - 1, pl.Height - 1}, {-pl.Border, -pl.Border}, {pl.Width + pl.Border - 1, pl.Height + pl.Border - 1}} {
						for mvY := -9; mvY <= 9; mvY++ {
							for mvX := -9; mvX <= 9; mvX++ {
								var got, want uint16
								if c == 0 {
									if format.BitDepth() == 10 {
										got = prediction.LumaSampleHigh(pl, pos[0], pos[1], mvX, mvY)
										want = prediction.LumaSampleHigh(rp, pos[0], pos[1], mvX, mvY)
									} else {
										got = uint16(prediction.LumaSample(pl, pos[0], pos[1], mvX, mvY))
										want = uint16(prediction.LumaSample(rp, pos[0], pos[1], mvX, mvY))
									}
								} else if format.BitDepth() == 10 {
									got = prediction.ChromaSampleFormatHigh(pl, pos[0], pos[1], mvX, mvY, subW, subH)
									want = prediction.ChromaSampleFormatHigh(rp, pos[0], pos[1], mvX, mvY, subW, subH)
								} else {
									got = uint16(prediction.ChromaSampleFormat(pl, pos[0], pos[1], mvX, mvY, subW, subH))
									want = uint16(prediction.ChromaSampleFormat(rp, pos[0], pos[1], mvX, mvY, subW, subH))
								}
								if got != want {
									t.Fatalf("parity %d plane %d pos %v mv %d,%d: %d want %d", parity, c, pos, mvX, mvY, got, want)
								}
							}
						}
					}
				}
			}
		})
	}
}
