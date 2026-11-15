package prediction

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
)

func TestAllocation_MCBlock(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		t.Run(format.String(), func(t *testing.T) {
			pic, err := picture.New(2, 4, format)
			if err != nil {
				t.Fatal(err)
			}
			defer pic.Release()
			value := uint16(173)
			if format.BitDepth() == 10 {
				value = 701
			}
			for y := range pic.Height {
				for x := range pic.Width {
					pic.Planes[0].SetSample(x, y, value)
				}
			}
			field, err := picture.FieldView(pic, true)
			if err != nil {
				t.Fatal(err)
			}
			var dst8 [256]byte
			var dst16 [256]uint16
			for _, ref := range []*picture.Plane{&pic.Planes[0], &field.Planes[0]} {
				for _, position := range [][2]int{{4, 4}, {-2, -3}, {30, 30}} {
					if got := testing.AllocsPerRun(100, func() {
						for _, sampling := range [][2]int{{1, 1}, {2, 2}, {2, 1}} {
							if format.BitDepth() == 8 {
								dst8[0], dst8[255] = 0, 0
								MCBlockChromaFormat(dst8[:], 16, ref, position[0], position[1], 16, 16, 2, 2, sampling[0], sampling[1])
								if dst8[0] != byte(value) || dst8[255] != byte(value) {
									panic("常量块解析值改变或未执行MC")
								}
							} else {
								dst16[0], dst16[255] = 0, 0
								MCBlockChromaFormatHigh(dst16[:], 16, ref, position[0], position[1], 16, 16, 2, 2, sampling[0], sampling[1])
								if dst16[0] != value || dst16[255] != value {
									panic("高位深常量块解析值改变或未执行MC")
								}
							}
						}
					}); got != 0 {
						t.Fatalf("位置%v分配=%v，期望0", position, got)
					}
				}
			}
		})
	}
}
