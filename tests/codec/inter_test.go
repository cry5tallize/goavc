package codec_test

import (
	"bytes"
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"github.com/goavc/h264/nal"
	"testing"
)

func movingFrame(w, h, index int) *h264.Frame {
	f := pattern(w, h)
	f.PTS = int64(index) * 1001
	f.Duration = 1001
	for c := range f.Planes {
		cw, ch := f.Format.PlaneDimensions(c, w, h)
		for y := 0; y < ch; y++ {
			for x := 0; x < cw; x++ {
				v := byte((x+index)*13 + y*23 + (x+index)/8*17 + c*41)
				if index >= 20 {
					v = 255 - v
				}
				f.Planes[c].Data[y*cw+x] = v
			}
		}
	}
	return f
}

func TestEncoder_InterSequenceOracle(t *testing.T) {
	for _, qp := range []int{0, 26, 51} {
		for _, slices := range []int{1, 3, 5} {
			t.Run(fmt.Sprintf("qp%d/slices%d", qp, slices), func(t *testing.T) {
				c := h264.DefaultEncoderConfig()
				c.Width = 64
				c.Height = 48
				c.QP = qp
				c.GOPSize = 17
				e, err := encoder.New(c, encoder.WithSliceCount(slices), encoder.WithSearchRange(8))
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
				idr, pframes := 0, 0
				for i := 0; i < 40; i++ {
					f := movingFrame(c.Width, c.Height, i)
					if i == 33 {
						f.KeyFrame = true
					}
					if err = e.SendFrame(f); err != nil {
						t.Fatal(err)
					}
					packet, err := e.ReceivePacket()
					if err != nil {
						t.Fatal(err)
					}
					recon, err := e.Reconstruction()
					if err != nil {
						t.Fatal(err)
					}
					isIDR := i == 0 || i == 17 || i == 33
					if packet.KeyFrame != isIDR || recon.KeyFrame != isIDR || packet.PTS != f.PTS || packet.DTS != f.PTS || packet.Duration != 1001 {
						t.Fatalf("frame %d metadata %+v", i, packet)
					}
					scan := nal.NewAnnexBScanner(bytes.NewReader(packet.Data))
					for {
						u, err := scan.Next()
						if err != nil {
							break
						}
						if u.Header.Type == nal.TypeSliceIDR {
							idr++
						}
						if u.Header.Type == nal.TypeSlice {
							pframes++
						}
					}
					if err = d.SendPacket(packet); err != nil {
						t.Fatalf("frame %d: %v", i, err)
					}
					got, err := d.ReceiveFrame()
					if err != nil {
						t.Fatal(err)
					}
					a, b := packed(got), packed(recon)
					if !bytes.Equal(a, b) {
						for pos := range a {
							if a[pos] != b[pos] {
								t.Fatalf("frame %d pixel %d got %d local %d", i, pos, a[pos], b[pos])
							}
						}
					}
					if got.PTS != f.PTS {
						t.Fatal("PTS", got.PTS, f.PTS)
					}
					stream = append(stream, packet.Data...)
					want = append(want, b...)
					got.Release()
					recon.Release()
					f.Release()
				}
				if idr != 3*slices || pframes != 37*slices {
					t.Fatal("NAL picture types", idr, pframes)
				}
				ffmpegCheck(t, stream, want)
			})
		}
	}
}
