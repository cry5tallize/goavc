package encoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"testing"
)

func testFrame(w, h int) *h264.Frame {
	f := &h264.Frame{Width: w, Height: h, Format: h264.YUV420P, PTS: 17, Duration: 3}
	for c := range f.Planes {
		cw, ch := f.Format.PlaneDimensions(c, w, h)
		f.Planes[c] = h264.Plane{Data: make([]byte, (cw+3)*ch), Stride: cw + 3}
		for y := 0; y < ch; y++ {
			for x := 0; x < cw; x++ {
				f.Planes[c].Data[y*(cw+3)+x] = byte(c*51 + x*3 + y*7)
			}
		}
	}
	return f
}

func TestEncoder_StateAndOwnership(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 34
	c.Height = 18
	e, err := New(c, WithPCM(), WithSliceCount(3))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err = e.ReceivePacket(); !errors.Is(err, h264.ErrAgain) {
		t.Fatal(err)
	}
	if _, err = e.Reconstruction(); !errors.Is(err, h264.ErrAgain) {
		t.Fatal(err)
	}
	f := testFrame(34, 18)
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrAgain) {
		t.Fatal(err)
	}
	old := f.Planes[0].Data[0]
	clear(f.Planes[0].Data)
	r, err := e.Reconstruction()
	if err != nil {
		t.Fatal(err)
	}
	if r.PTS != 17 || r.Duration != 3 {
		t.Fatal("reconstruction timestamps", r.PTS, r.Duration)
	}
	if r.Planes[0].Data[1] != 3 || r.Planes[0].Data[0] != old {
		t.Fatal("input was retained")
	}
	r.Planes[0].Data[1] = 0
	r.Release()
	r, _ = e.Reconstruction()
	if r.Planes[0].Data[1] != 3 {
		t.Fatal("reconstruction aliases internal buffer")
	}
	if err = e.Flush(); err != nil {
		t.Fatal(err)
	}
	if err = e.Flush(); err != nil {
		t.Fatal(err)
	}
	p, err := e.ReceivePacket()
	if err != nil || p.PTS != 17 || p.DTS != 17 || p.Duration != 3 || !p.KeyFrame {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err = e.ReceivePacket(); !errors.Is(err, h264.ErrEOF) {
		t.Fatal(err)
	}
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrEOF) {
		t.Fatal(err)
	}
	if err = e.Reset(); err != nil {
		t.Fatal(err)
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e.Close()
	if err = e.Reset(); !errors.Is(err, h264.ErrEOF) {
		t.Fatal(err)
	}
}

func TestEncoder_Configuration(t *testing.T) {
	base := h264.DefaultEncoderConfig()
	base.Width = 64
	base.Height = 48
	for _, change := range []func(*h264.EncoderConfig){
		func(c *h264.EncoderConfig) { c.Profile = h264.ProfileMain },
		func(c *h264.EncoderConfig) { c.Threads = 2 },
		func(c *h264.EncoderConfig) { c.Width = 65 },
		func(c *h264.EncoderConfig) { c.RateControl = h264.RateControlABR; c.Bitrate = 100000 },
		func(c *h264.EncoderConfig) { c.Level = 10 },
	} {
		c := base
		change(&c)
		if _, err := New(c, WithPCM()); !errors.Is(err, h264.ErrUnsupported) {
			t.Fatal(c, err)
		}
	}
	if _, err := New(base, WithSliceCount(65)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	e, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SendFrame(testFrame(16, 16)); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if err = e.SendFrame(&h264.Frame{}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}
