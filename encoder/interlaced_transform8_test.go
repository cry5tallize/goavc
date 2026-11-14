package encoder

import (
	"testing"

	h264 "github.com/goavc/h264"
)

// 不依赖内容碰巧选中8×8：以同一场参考加常量亮度，产生真实非零DC残差。
func TestInterlacedActualTransform8Decision(t *testing.T) {
	for _, entropy := range []bool{false, true} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height, cfg.Profile, cfg.QP = 32, 64, h264.ProfileHigh, 18
		e, err := New(cfg, WithInterlaced(true), WithCABAC(entropy), WithTransform8x8(), WithSearchRange(2))
		if err != nil {
			t.Fatal(err)
		}
		f := testFrame(cfg.Width, cfg.Height)
		for comp := range f.Planes {
			w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
			for y := range h {
				for x := range w {
					f.Planes[comp].Data[y*f.Planes[comp].Stride+x] = byte(30 + (x*17+y/2*23+comp*29+y%2*37)%160)
				}
			}
		}
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		if _, err = e.ReceivePacket(); err != nil {
			t.Fatal(err)
		}
		r, err := e.Reconstruction()
		if err != nil {
			t.Fatal(err)
		}
		r.KeyFrame = false
		for comp := range r.Planes {
			for index, value := range r.Planes[comp].Data {
				r.Planes[comp].Data[index] = value + 12
			}
		}
		if err = e.SendFrame(r); err != nil {
			t.Fatal(err)
		}
		r.Release()
		selected := 0
		for _, s := range e.last.fields {
			for _, info := range s.info {
				if info.inter && info.transform8 && info.cbp != 0 {
					selected++
				}
			}
		}
		if selected == 0 {
			t.Fatal("真实场P宏块未使用非零8×8残差")
		}
		e.Close()
	}
}
