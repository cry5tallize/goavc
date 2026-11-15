package codec_test

import (
	"bytes"
	"fmt"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
)

func TestEncoder_CABACPCM_RoundTrip(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height, c.Profile = 16, 16, h264.ProfileMain
	e, err := encoder.New(c, encoder.WithPCM())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := pattern(c.Width, c.Height)
	for comp := range f.Planes {
		for i, value := range f.Planes[comp].Data {
			if value == 0 {
				f.Planes[comp].Data[i] = 1
			}
		}
	}
	de, err := decoder.New(h264.DefaultDecoderConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer de.Close()
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	p, err := e.ReceivePacket()
	if err != nil {
		t.Fatal(err)
	}
	if err = de.SendPacket(p); err != nil {
		t.Fatal(err)
	}
	got, err := de.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	defer got.Release()
	if !bytes.Equal(packed(got), packed(f)) {
		t.Fatal("CABAC PCM 输出与输入不一致")
	}
}

// 非均匀运动、场景切换、frame_num 回绕及宏块 QP 变化同时覆盖上下文连续性。
// 独立 FFmpeg 输出还必须与共享内核的本地重建逐像素一致。
func TestEncoder_CABACSequenceOracle(t *testing.T) {
	for _, profile := range []string{h264.ProfileMain, h264.ProfileHigh} {
		for _, qp := range []int{0, 26, 51} {
			for _, slices := range []int{1, 3} {
				t.Run(fmt.Sprintf("profile%s/qp%d/slices%d", profile, qp, slices), func(t *testing.T) {
					cfg := h264.DefaultEncoderConfig()
					cfg.Width, cfg.Height, cfg.GOPSize, cfg.QP, cfg.Profile = 64, 48, 17, qp, profile
					e, err := encoder.New(cfg, encoder.WithSliceCount(slices), encoder.WithSearchRange(8), encoder.WithMacroblockQP(func(x, y int) int { return min(51, max(0, qp+(x+y)%3-1)) }))
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
					for frame := 0; frame < 24; frame++ {
						f := movingFrame(cfg.Width, cfg.Height, frame)
						if err = e.SendFrame(f); err != nil {
							t.Fatalf("帧%d: %v", frame, err)
						}
						f.Release()
						p, err := e.ReceivePacket()
						if err != nil {
							t.Fatal(err)
						}
						r, err := e.Reconstruction()
						if err != nil {
							t.Fatal(err)
						}
						if err = d.SendPacket(p); err != nil {
							t.Fatalf("帧%d: %v", frame, err)
						}
						got, err := d.ReceiveFrame()
						if err != nil {
							t.Fatal(err)
						}
						a, b := packed(got), packed(r)
						if !bytes.Equal(a, b) {
							for pos := range a {
								if a[pos] != b[pos] {
									t.Fatalf("帧%d 采样%d: 解码%d 重建%d", frame, pos, a[pos], b[pos])
								}
							}
						}
						stream, want = append(stream, p.Data...), append(want, b...)
						got.Release()
						r.Release()
					}
					ffmpegCheck(t, stream, want)
				})
			}
		}
	}
}
