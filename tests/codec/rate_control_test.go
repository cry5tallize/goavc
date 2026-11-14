package codec_test

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/encoder"
)

func TestEncoder_ABRLongSequenceAccuracy(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.GOPSize = 32, 32, 30
	cfg.RateControl, cfg.Bitrate = h264.RateControlABR, 60_000
	e, err := encoder.New(cfg, encoder.WithSearchRange(4))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var bits int64
	var stream, want []byte
	qpSeen := map[int]bool{}
	for i := 0; i < 120; i++ {
		f := movingFrame(cfg.Width, cfg.Height, i)
		if err = e.SendFrame(f); err != nil {
			t.Fatalf("frame %d: %v", i, err)
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
		qpSeen[e.LastQP()] = true
		bits += int64(len(p.Data)) * 8
		stream = append(stream, p.Data...)
		want = append(want, packed(r)...)
		r.Release()
	}
	actual := bits * int64(cfg.FPSNum) / (120 * int64(cfg.FPSDen))
	delta := actual - int64(cfg.Bitrate)
	if delta < 0 {
		delta = -delta
	}
	if delta*100 > int64(cfg.Bitrate)*10 {
		t.Fatalf("ABR rate %d bps differs from target %d by more than 10%%", actual, cfg.Bitrate)
	}
	if len(qpSeen) < 2 {
		t.Fatal("ABR did not adjust QP")
	}
	ffmpegCheck(t, stream, want)
}

func TestEncoder_CBRWindowRate(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.GOPSize = 32, 32, 30
	cfg.RateControl, cfg.Bitrate = h264.RateControlCBR, 60_000
	e, err := encoder.New(cfg, encoder.WithSearchRange(4))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var window int64
	var stream, want []byte
	for i := 0; i < 60; i++ {
		f := movingFrame(cfg.Width, cfg.Height, i)
		if err = e.SendFrame(f); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		f.Release()
		p, err := e.ReceivePacket()
		if err != nil {
			t.Fatal(err)
		}
		window += int64(len(p.Data)) * 8
		stream = append(stream, p.Data...)
		r, err := e.Reconstruction()
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, packed(r)...)
		r.Release()
		if i == 29 {
			checkRateWindow(t, window, cfg.Bitrate, cfg.FPSNum, cfg.FPSDen, 30)
			window = 0
		}
	}
	checkRateWindow(t, window, cfg.Bitrate, cfg.FPSNum, cfg.FPSDen, 30)
	ffmpegCheck(t, stream, want)
}

func checkRateWindow(t *testing.T, bits int64, bitrate, fpsNum, fpsDen, frames int) {
	t.Helper()
	actual := bits * int64(fpsNum) / (int64(frames) * int64(fpsDen))
	delta := actual - int64(bitrate)
	if delta < 0 {
		delta = -delta
	}
	if delta*100 > int64(bitrate)*10 {
		t.Fatalf("rate window %d bps differs from target %d by more than 10%%", actual, bitrate)
	}
}

func TestEncoder_VBVFailureIsTransactional(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 64, 48
	cfg.RateControl, cfg.Bitrate, cfg.VBVBufferSize = h264.RateControlVBV, 1000, 1
	e, err := encoder.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	beforeQP, beforeBits := e.LastQP(), e.LastOutputBits()
	if err = e.SendFrame(movingFrame(cfg.Width, cfg.Height, 0)); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatalf("tiny VBV accepted frame: %v", err)
	}
	if e.LastQP() != beforeQP || e.LastOutputBits() != beforeBits {
		t.Fatal("failed VBV frame changed rate-control state")
	}
}

func TestEncoder_VBVSequenceStaysWithinBuffer(t *testing.T) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height, cfg.GOPSize = 32, 32, 30
	cfg.RateControl, cfg.Bitrate, cfg.VBVBufferSize = h264.RateControlVBV, 60_000, 60_000
	e, err := encoder.New(cfg, encoder.WithSearchRange(4))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var stream, want []byte
	for i := 0; i < 60; i++ {
		f := movingFrame(cfg.Width, cfg.Height, i)
		if err = e.SendFrame(f); err != nil {
			t.Fatalf("frame %d: %v", i, err)
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
		stats := e.RateControlStats()
		if stats.QP < 0 || stats.QP > 51 || stats.LastFrameBits != int64(len(p.Data))*8 || stats.VBVFullness < 0 || stats.VBVFullness > stats.VBVBufferBits {
			t.Fatalf("frame %d VBV state %+v", i, stats)
		}
		stream = append(stream, p.Data...)
		want = append(want, packed(r)...)
		r.Release()
	}
	ffmpegCheck(t, stream, want)
}

// 静态画面即使QP=0也远低于目标码率，必须用规范filler补足而非伪称CBR。
func TestEncoder_StaticFillerOracle(t *testing.T) {
	for _, rc := range []h264.RateControl{h264.RateControlCBR, h264.RateControlVBV} {
		t.Run(rc.String(), func(t *testing.T) {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height = 32, 32
			cfg.Profile = h264.ProfileMain
			cfg.RateControl = rc
			cfg.Bitrate = 600_001
			cfg.FPSNum, cfg.FPSDen = 30000, 1001
			cfg.VBVBufferSize = 600_001
			e, err := encoder.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			var stream, want []byte
			for i := 0; i < 60; i++ {
				f := movingFrame(32, 32, i)
				for comp := range f.Planes {
					for j := range f.Planes[comp].Data {
						f.Planes[comp].Data[j] = 128
					}
				}
				if err = e.SendFrame(f); err != nil {
					t.Fatal(err)
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
				stream = append(stream, p.Data...)
				want = append(want, packed(r)...)
				r.Release()
				stats := e.RateControlStats()
				if rc == h264.RateControlVBV && (stats.VBVFullness < 0 || stats.VBVFullness > stats.VBVBufferBits) {
					t.Fatal("静态VBV越界", stats)
				}
			}
			actual := int64(len(stream)) * 8 * int64(cfg.FPSNum) / (60 * int64(cfg.FPSDen))
			delta := actual - int64(cfg.Bitrate)
			if delta < 0 {
				delta = -delta
			}
			if delta*100 > int64(cfg.Bitrate) {
				t.Fatalf("静态填充码率%d，目标%d，超过1%%", actual, cfg.Bitrate)
			}
			ffmpegCheck(t, stream, want)
		})
	}
}
