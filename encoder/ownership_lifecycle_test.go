package encoder

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

// 通过公开SendFrame在不同组合的后续通道拒绝QP，验证完整图像的唯一owner。
// FieldView/独立分量描述符只能借用；失败不能遗留租约，成功不能重复归还。
func TestEncoder_CompositeFailureReturnsOwnedPicture(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		field, separate, cabac bool
		failAt                 int
	}{
		{"frame", false, false, false, 2},
		{"cabac", false, false, true, 2},
		{"second_field", true, false, false, 3},
		{"second_field_cabac", true, false, true, 3},
		{"second_plane", false, true, false, 5},
		{"second_plane_cabac", false, true, true, 5},
		{"second_plane_field", true, true, false, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height = 32, 32
			cfg.Profile, cfg.PixelFormat = h264.ProfileHigh444, h264.YUV444P
			calls, reject := 0, true
			options := []Option{WithCABAC(tc.cabac), WithMacroblockQP(func(x, y int) int {
				calls++
				if reject && calls == tc.failAt {
					return 52
				}
				return 26
			})}
			if tc.field {
				options = append(options, WithInterlaced(true))
			}
			if tc.separate {
				options = append(options, WithSeparateColourPlanes())
			}
			e, err := New(cfg, options...)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			f, err := h264.NewFrame(32, 32, cfg.PixelFormat)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Release()
			for comp := range f.Planes {
				for i := range f.Planes[comp].Data {
					f.Planes[comp].Data[i] = byte(40 + comp*40)
				}
			}
			if err = e.SendFrame(f); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(err)
			}
			gets, puts, _ := e.picturePool.Stats()
			if gets != puts {
				t.Fatalf("失败遗留图像: gets=%d puts=%d", gets, puts)
			}
			if e.reference != nil || e.out != nil {
				t.Fatal("失败提交了参考或输出")
			}
			reject = false
			if err = e.SendFrame(f); err != nil {
				t.Fatal(err)
			}
			if _, err = e.ReceivePacket(); err != nil {
				t.Fatal(err)
			}
			gets, puts, _ = e.picturePool.Stats()
			if gets-puts != 1 {
				t.Fatalf("成功重建必须唯一持有: gets=%d puts=%d", gets, puts)
			}
			if err = e.Reset(); err != nil {
				t.Fatal(err)
			}
			gets, puts, _ = e.picturePool.Stats()
			if gets != puts {
				t.Fatalf("Reset遗留图像: gets=%d puts=%d", gets, puts)
			}
		})
	}
}
