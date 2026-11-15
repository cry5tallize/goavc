package codec_test

import (
	"bytes"
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
	"testing"
)

func FuzzEncoder_RoundTrip(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0})
	f.Add([]byte{8, 8, 51, 3, 255, 255, 0, 255})
	f.Add([]byte{1, 1, 26, 1, 0, 99, 220, 43})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}
		c := h264.DefaultEncoderConfig()
		c.Width = 16 + int(data[0]%9)*2
		c.Height = 16 + int(data[1]%9)*2
		c.QP = int(data[2] % 52)
		mbs := (c.Width + 15) / 16 * ((c.Height + 15) / 16)
		slices := 1 + int(data[3])%mbs
		options := []encoder.Option{encoder.WithSliceCount(slices)}
		if data[3]&128 != 0 {
			options = append(options, encoder.WithPCM())
		}
		e, err := encoder.New(c, options...)
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
		for comp := range input.Planes {
			for i := range input.Planes[comp].Data {
				input.Planes[comp].Data[i] = data[(i+comp)%len(data)]
			}
		}
		if data[3]&128 != 0 {
			nonzeroPCM(input)
		}
		if err = e.SendFrame(input); err != nil {
			if !errors.Is(err, h264.ErrUnsupported) || c.QP >= 4 || data[3]&128 != 0 {
				t.Fatal(err)
			}
			// Annex A 的极低 QP 前缀/零 PCM 边界可显式拒绝；检查调用方修正
			// 输入后仍能编码合法 IDR，不把不支持算作成功编解码。
			for comp := range input.Planes {
				for i := range input.Planes[comp].Data {
					input.Planes[comp].Data[i] = 128
				}
			}
			if err = e.SendFrame(input); err != nil {
				t.Fatal(err)
			}
		}
		p, err := e.ReceivePacket()
		if err != nil {
			t.Fatal(err)
		}
		r, err := e.Reconstruction()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if err = d.SendPacket(p); err != nil {
			t.Fatal(err)
		}
		got, err := d.ReceiveFrame()
		if err != nil {
			t.Fatal(err)
		}
		defer got.Release()
		if !bytes.Equal(packed(got), packed(r)) {
			t.Fatal("reconstruction differs")
		}
	})
}
