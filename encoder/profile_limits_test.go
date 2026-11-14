package encoder

import (
	"errors"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"testing"
)

func TestProfileLimits_PCMZeroIsTransactional(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 16
	c.Height = 16
	e, err := New(c, WithPCM())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := testFrame(16, 16)
	f.Planes[0].Data[0] = 0
	if err = e.SendFrame(f); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("Baseline PCM zero must be rejected", err)
	}
	if e.out != nil || e.reference != nil || e.idr != 0 {
		t.Fatal("unsupported PCM changed state")
	}
}

func TestProfileLimits_CAVLCPrefixBound(t *testing.T) {
	var levels [16]int32
	levels[0] = 1000000
	w := bitstream.NewWriter()
	w.WriteBit(1)
	before := w.BitsWritten()
	if _, err := cavlc.EncodeBlockLimited(w, &levels, cavlc.LumaDC, 0, 15); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal(err)
	}
	if w.BitsWritten() != before {
		t.Fatal("profile error wrote partial block")
	}
	if _, err := cavlc.EncodeBlockLimited(w, &levels, cavlc.LumaDC, 0, 28); err != nil {
		t.Fatal("High escape rejected", err)
	}
}

func TestProfileLimits_LowQPExtremeContentRecovers(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 176
	c.Height = 144
	c.QP = 0
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	// 大幅环绕斜坡触发色度 DC 的长前缀，同时块内含0而不能改用 Baseline PCM。
	if err = e.SendFrame(testFrame(c.Width, c.Height)); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("expected constrained low-QP rejection", err)
	}
	if e.out != nil || e.reference != nil || e.frameNum != 0 {
		t.Fatal("unsupported content changed state")
	}
	valid := testFrame(c.Width, c.Height)
	for comp := range valid.Planes {
		for i := range valid.Planes[comp].Data {
			valid.Planes[comp].Data[i] = 128
		}
	}
	if err = e.SendFrame(valid); err != nil {
		t.Fatal("valid retry failed", err)
	}
	p, err := e.ReceivePacket()
	if err != nil || !p.KeyFrame {
		t.Fatal(err, p.KeyFrame)
	}
}
