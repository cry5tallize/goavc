package encoder

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/nal"
)

func TestRateFiller_ExactBytesAndResourceLimit(t *testing.T) {
	e := Encoder{cfg: h264.EncoderConfig{RateControl: h264.RateControlCBR}, maxAUBytes: 100}
	out, err := e.padRateControlledAU([]byte{0x80}, 80)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x80, 0, 0, 0, 1, 12, 255, 255, 255, 128}
	if !bytes.Equal(out, want) {
		t.Fatalf("filler字节%v，期望%v", out, want)
	}
	scanner := nal.NewAnnexBScanner(bytes.NewReader(out[1:]))
	unit, err := scanner.Next()
	if err != nil {
		t.Fatal(err)
	}
	if unit.Header.Type != nal.TypeFiller || unit.Header.RefIDC != 0 {
		t.Fatal("filler NAL header错误")
	}
	if _, err = e.padRateControlledAU([]byte{0x80}, 801); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("超限填充未拒绝", err)
	}
}

func TestRateControl_FractionalBudgetCarriesRemainder(t *testing.T) {
	e := Encoder{cfg: h264.EncoderConfig{RateControl: h264.RateControlABR, Bitrate: 1001, FPSNum: 30, FPSDen: 1}}
	var total int64
	for range 30 {
		total += e.bitsPerFrame()
		e.commitRateControl(4)
	}
	if total != 1001 || e.rcRemainder != 0 {
		t.Fatalf("分数预算累计%d，余数%d", total, e.rcRemainder)
	}
}

func TestRateFiller_PaddingDoesNotSteerQPUpwards(t *testing.T) {
	for _, rc := range []h264.RateControl{h264.RateControlCBR, h264.RateControlVBV} {
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height = 32, 32
		cfg.Profile = h264.ProfileMain
		cfg.RateControl = rc
		cfg.Bitrate = 600_000
		cfg.VBVBufferSize = 600_000
		e, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		f := testFrame(32, 32)
		for comp := range f.Planes {
			for i := range f.Planes[comp].Data {
				f.Planes[comp].Data[i] = 128
			}
		}
		if err = e.SendFrame(f); err != nil {
			t.Fatal(err)
		}
		if e.LastQP() != 0 {
			t.Fatalf("%s填充掩盖了低QP可行候选：QP=%d", rc, e.LastQP())
		}
		e.Close()
		f.Release()
	}
}
