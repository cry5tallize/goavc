package encoder

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func BenchmarkEncoderCBRFlatSequence_720p_Generic(b *testing.B) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height, c.Profile = 1280, 720, h264.ProfileHigh
	c.GOPSize, c.IDRInterval, c.MaxBFrames = 600, 600, 2
	c.RateControl, c.Bitrate, c.VBVBufferSize = h264.RateControlCBR, 2_000_000, 2_000_000
	e, err := New(c, WithTransform8x8())
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := &h264.Frame{Width: c.Width, Height: c.Height, Format: h264.YUV420P, Duration: 1}
	for comp := range f.Planes {
		w, h := f.Format.PlaneDimensions(comp, c.Width, c.Height)
		f.Planes[comp] = h264.Plane{Data: make([]byte, w*h), Stride: w}
		for i := range f.Planes[comp].Data {
			f.Planes[comp].Data[i] = 128
		}
	}
	defer f.Release()
	drain := func() {
		for {
			_, err := e.ReceivePacket()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportAllocs()
	b.SetBytes(12 * 1280 * 720 * 3 / 2)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = e.Reset(); err != nil {
			b.Fatal(err)
		}
		// 一个op含12帧和排空，必须覆盖同帧型的冷启动与后续反馈搜索。
		for frame := 0; frame < 12; frame++ {
			f.PTS = int64(frame)
			if err = e.SendFrame(f); err != nil {
				b.Fatal(err)
			}
			drain()
		}
		if err = e.Flush(); err != nil {
			b.Fatal(err)
		}
		drain()
	}
}
