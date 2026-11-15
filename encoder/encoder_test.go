package encoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"testing"
)

func TestEncoder_LevelBudgetIsTransactional(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 1920
	c.Height = 1080
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	// 注入一个极小预算检查“写完再发布”边界，不分配或产出超限码流。
	e.maxAUBytes = 1
	if err = e.SendFrame(testFrame(c.Width, c.Height)); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal(err)
	}
	if e.reference != nil || e.recon != nil || e.out != nil || e.idr != 0 || e.frameNum != 0 {
		t.Fatal("failed AU modified state")
	}
	gets, puts, _ := e.picturePool.Stats()
	if gets != puts {
		t.Fatalf("failed AU leaked pictures: gets=%d puts=%d", gets, puts)
	}
}

func TestEncoder_RateControlCandidatesReturnPictures(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 32, 32
	c.RateControl, c.Bitrate = h264.RateControlABR, 100_000
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.SendFrame(testFrame(c.Width, c.Height)); err != nil {
		t.Fatal(err)
	}
	gets, puts, _ := e.picturePool.Stats()
	if gets == 0 || gets-puts != 1 {
		t.Fatalf("candidate picture ownership: gets=%d puts=%d", gets, puts)
	}
	if err = e.Reset(); err != nil {
		t.Fatal(err)
	}
	gets, puts, _ = e.picturePool.Stats()
	if gets != puts {
		t.Fatalf("reset retained picture ownership: gets=%d puts=%d", gets, puts)
	}
}

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

func pcmFrame(w, h int) *h264.Frame {
	f := testFrame(w, h)
	for comp := range f.Planes {
		for i, v := range f.Planes[comp].Data {
			if v == 0 {
				f.Planes[comp].Data[i] = 1
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
	f := pcmFrame(34, 18)
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
	f = pcmFrame(34, 18)
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
		func(c *h264.EncoderConfig) { c.Threads = 2 },
		func(c *h264.EncoderConfig) { c.Width = 65 },
		func(c *h264.EncoderConfig) { c.Level = 10 },
	} {
		c := base
		change(&c)
		if _, err := New(c, WithPCM()); !errors.Is(err, h264.ErrUnsupported) {
			t.Fatal(c, err)
		}
	}
	for _, rc := range []h264.RateControl{h264.RateControlABR, h264.RateControlCBR, h264.RateControlVBV} {
		c := base
		c.RateControl, c.Bitrate = rc, 100_000
		if _, err := New(c); err != nil {
			t.Fatalf("rate control %s was rejected: %v", rc, err)
		}
	}
	c := base
	c.MaxBFrames = 1
	if _, err := New(c); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatalf("B frames must remain explicit unsupported: %v", err)
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
