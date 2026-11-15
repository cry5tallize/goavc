package codec_test

import (
	"bytes"
	"errors"
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
	"testing"
)

func TestEncoder_Scaling4x4DCAndBOracle(t *testing.T) {
	for _, qp := range []int{0, 26, 51} {
		for _, b := range []int{0, 2} {
			t.Run(fmt.Sprintf("qp%d/b%d", qp, b), func(t *testing.T) {
				cfg := h264.DefaultEncoderConfig()
				cfg.Width, cfg.Height = 34, 18
				cfg.Profile = h264.ProfileHigh
				cfg.MaxBFrames = b
				cfg.QP = qp
				cfg.GOPSize = 7
				var matrices [6][16]uint8
				for l := range matrices {
					for i := range matrices[l] {
						matrices[l][i] = uint8(8 + (l*3+i*5)%23)
					}
				}
				e, err := encoder.New(cfg, encoder.WithScalingMatrices4x4(matrices), encoder.WithSliceCount(3))
				if err != nil {
					t.Fatal(err)
				}
				defer e.Close()
				d, err := decoder.New(h264.DecoderConfig{})
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close()
				var stream []byte
				recons := map[int64][]byte{}
				decoded := 0
				check := func() {
					t.Helper()
					for {
						got, err := d.ReceiveFrame()
						if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
							return
						}
						if err != nil {
							t.Fatal(err)
						}
						if got.PTS != int64(decoded) || !bytes.Equal(packed(got), recons[got.PTS]) {
							t.Fatal("4×4/DC自定义矩阵解码不一致", got.PTS)
						}
						decoded++
						got.Release()
					}
				}
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
						r, err := e.Reconstruction()
						if err != nil {
							t.Fatal(err)
						}
						recons[p.PTS] = packed(r)
						r.Release()
						stream = append(stream, p.Data...)
						if err = d.SendPacket(p); err != nil {
							t.Fatal(err)
						}
						check()
					}
				}
				for i := 0; i < 21; i++ {
					f := movingFrame(34, 18, i)
					f.PTS = int64(i)
					f.Duration = 1
					if err = e.SendFrame(f); err != nil {
						t.Fatal(err)
					}
					f.Release()
					drain()
				}
				if err = e.Flush(); err != nil {
					t.Fatal(err)
				}
				drain()
				if err = d.Flush(); err != nil {
					t.Fatal(err)
				}
				check()
				if decoded != 21 {
					t.Fatal("帧数错误", decoded)
				}
				var want []byte
				for i := 0; i < 21; i++ {
					want = append(want, recons[int64(i)]...)
				}
				ffmpegCheck(t, stream, want)
			})
		}
	}
}
