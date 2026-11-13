package codec_test

import (
	"bytes"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
	"testing"
)

func FuzzEncoder_InterRoundTrip(f *testing.F) {
	f.Add([]byte{0, 26, 0, 1, 255, 99, 44})
	f.Add([]byte{1, 0, 3, 255, 0, 255, 0})
	f.Add([]byte{1, 51, 2, 128, 127, 126, 125})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}
		c := h264.DefaultEncoderConfig()
		c.Width = 16 + int(data[0]%2)*16
		c.Height = 16 + int(data[3]%2)*16
		c.QP = int(data[1] % 52)
		mbs := c.Width * c.Height / 256
		e, err := encoder.New(c, encoder.WithSliceCount(1+int(data[2])%mbs), encoder.WithSearchRange(4), encoder.WithMacroblockQP(func(x, y int) int { return int(data[(x+y)%len(data)] % 52) }))
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		d, err := decoder.New(h264.DecoderConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		input := pattern(c.Width, c.Height)
		for frame := 0; frame < 3; frame++ {
			input.PTS = int64(frame)
			for comp := range input.Planes {
				for i := range input.Planes[comp].Data {
					input.Planes[comp].Data[i] = data[(i+comp+frame)%len(data)]
				}
			}
			if err = e.SendFrame(input); err != nil {
				t.Fatal(frame, err)
			}
			p, err := e.ReceivePacket()
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.Reconstruction()
			if err != nil {
				t.Fatal(err)
			}
			if err = d.SendPacket(p); err != nil {
				t.Fatal(frame, err)
			}
			got, err := d.ReceiveFrame()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(packed(got), packed(r)) || got.PTS != input.PTS {
				t.Fatalf("frame %d differs", frame)
			}
			r.Release()
			got.Release()
		}
	})
}
