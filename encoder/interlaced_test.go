package encoder

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestInterlacedFieldStatesAndSkipInference(t *testing.T) {
	for _, entropy := range []bool{false, true} {
		for _, topFirst := range []bool{false, true} {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height, cfg.Profile, cfg.QP = 64, 64, h264.ProfileMain, 26
			e, err := New(cfg, WithInterlaced(topFirst), WithCABAC(entropy), WithSliceCount(3), WithSearchRange(2))
			if err != nil {
				t.Fatal(err)
			}
			f := testFrame(cfg.Width, cfg.Height)
			for comp := range f.Planes {
				w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
				for y := range h {
					for x := range w {
						f.Planes[comp].Data[y*f.Planes[comp].Stride+x] = byte(32 + y%2*96 + x/4 + y/2 + comp*17)
					}
				}
			}
			if err = e.SendFrame(f); err != nil {
				t.Fatal(err)
			}
			p, err := e.ReceivePacket()
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Data) >= cfg.Width*cfg.Height*3/2 {
				t.Fatal("隔行首帧未真实压缩")
			}
			for parity, s := range e.last.fields {
				if s == nil || !s.field || s.pic.MBHeight != 2 || s.pic.Height != 32 || s.pic.Planes[0].Stride != e.recon.Planes[0].Stride*2 {
					t.Fatalf("场%d未接真实半高双步长布局", parity)
				}
				nonPCM := 0
				for _, info := range s.info {
					if !info.pcm {
						nonPCM++
					}
				}
				if nonPCM == 0 {
					t.Fatalf("场%d只有PCM", parity)
				}
			}
			static, err := e.Reconstruction()
			if err != nil {
				t.Fatal(err)
			}
			static.KeyFrame = false
			if err = e.SendFrame(static); err != nil {
				t.Fatal(err)
			}
			static.Release()
			if _, err = e.ReceivePacket(); err != nil {
				t.Fatal(err)
			}
			skips := 0
			for _, s := range e.last.fields {
				for _, info := range s.info {
					if info.skip {
						skips++
					}
				}
			}
			if skips == 0 {
				t.Fatal("静止场未使用真实场P_Skip")
			}
			for slice := range e.slices {
				first := len(e.last.fields[0].info) * slice / e.slices
				if e.last.fields[0].info[first].skip {
					t.Fatal("slice首pair全skip将错误推导frame flag")
				}
			}
			for pair, top := range e.last.fields[0].info {
				if !top.skip || !e.last.fields[1].info[pair].skip {
					continue
				}
				mw := e.last.fields[0].pic.MBWidth
				left := pair%mw != 0 && pair-1 >= top.start
				above := pair-mw >= top.start
				if !left && !above {
					t.Fatalf("slice跨pair-row后的pair%d双方Skip没有可用field邻pair，将推导frame flag", pair)
				}
			}
			e.Close()
		}
	}
}

func TestInterlacedConfigurationConstraints(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 16, 32
	if e, err := New(cfg, WithInterlaced(true)); !errors.Is(err, h264.ErrUnsupported) {
		if e != nil {
			e.Close()
		}
		t.Fatal("Baseline场编码必须显式拒绝", err)
	}
	cfg.Profile, cfg.Height = h264.ProfileMain, 34
	if _, err := New(cfg, WithInterlaced(false)); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("420场裁剪高度必须为4的倍数", err)
	}
	cfg.Height = 32
	if _, err := New(cfg, WithSliceCount(2), WithInterlaced(true)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("slice不能把宏块对拆开", err)
	}
	cfg.Height = 16
	for _, options := range [][]Option{{WithSliceCount(1), WithInterlaced(true)}, {WithInterlaced(false), WithSliceCount(1)}} {
		e, err := New(cfg, options...)
		if err != nil {
			t.Fatal("有效选项不应依赖顺序", err)
		}
		if e.sps.FrameMbsOnlyFlag || !e.sps.MbAdaptiveFrameFieldFlag || e.sps.FrameCropBottomOffset != 4 {
			t.Fatal("短帧未按32行和场裁剪单位表示")
		}
		e.Close()
	}
}
