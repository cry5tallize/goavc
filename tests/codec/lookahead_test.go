package codec_test

import (
	"bytes"
	"errors"
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"testing"
)

func TestEncoder_LookaheadFlashSceneAndDrainOracle(t *testing.T) {
	for _, bframes := range []int{0, 2} {
		t.Run(fmt.Sprintf("b%d", bframes), func(t *testing.T) {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height = 32, 32
			cfg.GOPSize, cfg.IDRInterval = 100, 100
			if bframes > 0 {
				cfg.Profile = h264.ProfileMain
				cfg.MaxBFrames = bframes
			}
			e, err := encoder.New(cfg, encoder.WithLookahead(2))
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			d, err := decoder.New(h264.DecoderConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			var stream, want []byte
			recons := map[int64][]byte{}
			var keys []int64
			received := 0
			drain := func() {
				t.Helper()
				for {
					p, err := e.ReceivePacket()
					if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if bframes == 0 && (p.PTS != int64(received)*100 || p.DTS != p.PTS) || p.Duration != 100 {
						t.Fatalf("输出顺序/时间戳错误：%+v", p)
					}
					if p.KeyFrame {
						keys = append(keys, p.PTS)
					}
					r, err := e.Reconstruction()
					if err != nil {
						t.Fatal(err)
					}
					if err = d.SendPacket(p); err != nil {
						t.Fatal(err)
					}
					got, err := d.ReceiveFrame()
					if bframes == 0 {
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(packed(got), packed(r)) {
							t.Fatal("Lookahead重建不一致")
						}
						got.Release()
					} else if err == nil {
						got.Release()
					} else if !errors.Is(err, h264.ErrAgain) {
						t.Fatal(err)
					}
					stream = append(stream, p.Data...)
					if bframes == 0 {
						want = append(want, packed(r)...)
					} else {
						recons[p.PTS] = packed(r)
					}
					r.Release()
					received++
				}
			}
			for i := 0; i < 10; i++ {
				f := movingFrame(32, 32, i)
				f.PTS = int64(i) * 100
				f.Duration = 100
				value := byte(40)
				if i == 3 || i >= 6 {
					value = 210
				}
				for comp := range f.Planes {
					for j := range f.Planes[comp].Data {
						f.Planes[comp].Data[j] = value
					}
				}
				if err = e.SendFrame(f); err != nil {
					t.Fatal(err)
				}
				// 修改并释放调用方缓冲，验证队列深拷贝。
				for comp := range f.Planes {
					clear(f.Planes[comp].Data)
				}
				f.Release()
				if i < 2 && bframes == 0 {
					if _, err = e.ReceivePacket(); !errors.Is(err, h264.ErrAgain) {
						t.Fatal("未达到窗口即输出", err)
					}
				} else {
					drain()
				}
			}
			if err = e.Flush(); err != nil {
				t.Fatal(err)
			}
			if err = e.Flush(); err != nil {
				t.Fatal(err)
			}
			drain()
			if received != 10 || len(keys) != 2 || keys[0] != 0 || keys[1] != 600 {
				t.Fatalf("帧数%d IDR%v，期望10帧/0,600", received, keys)
			}
			if _, err = e.ReceivePacket(); !errors.Is(err, h264.ErrEOF) {
				t.Fatal(err)
			}
			if bframes > 0 {
				for i := 0; i < 10; i++ {
					want = append(want, recons[int64(i)*100]...)
				}
			}
			ffmpegCheck(t, stream, want)
		})
	}
}
