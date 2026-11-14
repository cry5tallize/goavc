package codec_test

import (
	"bytes"
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"testing"
)

func TestEncoder_Transform8x8Oracle(t *testing.T) {
	for _, qp := range []int{0, 18, 26, 51} {
		for _, slices := range []int{1, 3} {
			for _, custom := range []bool{false, true} {
				t.Run(fmt.Sprintf("qp%d/slices%d/custom%t", qp, slices, custom), func(t *testing.T) {
					cfg := h264.DefaultEncoderConfig()
					cfg.Profile = h264.ProfileHigh
					cfg.Width, cfg.Height = 34, 18
					cfg.QP = qp
					cfg.GOPSize, cfg.IDRInterval = 5, 13
					options := []encoder.Option{encoder.WithTransform8x8(), encoder.WithSliceCount(slices), encoder.WithSearchRange(4)}
					if custom {
						var a, b [64]uint8
						for i := range a {
							a[i] = uint8(8 + i%17)
							b[i] = uint8(9 + i%23)
						}
						options = append(options, encoder.WithScalingMatrix8x8(a, b))
						var four [6][16]uint8
						for list := range four {
							for i := range four[list] {
								four[list][i] = uint8(8 + (i*3+list*5)%31)
							}
						}
						options = append(options, encoder.WithScalingMatrices4x4(four))
					}
					e, err := encoder.New(cfg, options...)
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
					for frame := 0; frame < 20; frame++ {
						var f *h264.Frame
						if frame%5 == 0 {
							f = movingFrame(cfg.Width, cfg.Height, frame)
						} else {
							f, err = e.Reconstruction()
							if err != nil {
								t.Fatal(err)
							}
							f.KeyFrame = false
							for comp := range f.Planes {
								for i, v := range f.Planes[comp].Data {
									f.Planes[comp].Data[i] = byte(min(255, int(v)+12))
								}
							}
						}
						if err = e.SendFrame(f); err != nil {
							t.Fatalf("帧%d：%v", frame, err)
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
							t.Fatalf("帧%d：%v", frame, err)
						}
						got, err := d.ReceiveFrame()
						if err != nil {
							t.Fatal(err)
						}
						a, b := packed(got), packed(r)
						if !bytes.Equal(a, b) {
							for pos := range a {
								if a[pos] != b[pos] {
									t.Fatalf("帧%d 采样%d 解码%d 重建%d", frame, pos, a[pos], b[pos])
								}
							}
						}
						stream = append(stream, p.Data...)
						want = append(want, b...)
						got.Release()
						r.Release()
					}
					ffmpegCheck(t, stream, want)
				})
			}
		}
	}
}
