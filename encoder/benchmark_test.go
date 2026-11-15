package encoder

import (
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/testutil/pattern"
	"testing"
)

func benchmarkEncoder(b *testing.B, w, h, fps int, pcm bool) {
	c := h264.DefaultEncoderConfig()
	c.Width = w
	c.Height = h
	c.QP = 26
	c.GOPSize = 1
	c.FPSNum = fps
	var options []Option
	if pcm {
		options = append(options, WithPCM())
	}
	e, err := New(c, options...)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := pattern.Frame(w, h, 1)
	defer f.Release()
	b.ReportAllocs()
	b.SetBytes(int64(w * h * 3 / 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = e.SendFrame(f); err != nil {
			b.Fatal(err)
		}
		if _, err = e.ReceivePacket(); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkEncoder_720p_Generic(b *testing.B)    { benchmarkEncoder(b, 1280, 720, 30, false) }
func BenchmarkEncoder_1080p_Generic(b *testing.B)   { benchmarkEncoder(b, 1920, 1080, 15, false) }
func BenchmarkEncoderPCM_720p_Generic(b *testing.B) { benchmarkEncoder(b, 1280, 720, 30, true) }

func BenchmarkEncoderP_720p_Generic(b *testing.B) {
	c := h264.DefaultEncoderConfig()
	c.Width = 1280
	c.Height = 720
	c.GOPSize = 600
	e, err := New(c)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := pattern.Frame(c.Width, c.Height, 1)
	defer f.Release()
	if err = e.SendFrame(f); err != nil {
		b.Fatal(err)
	}
	e.ReceivePacket()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = e.SendFrame(f); err != nil {
			b.Fatal(err)
		}
		if _, err = e.ReceivePacket(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncoderPFlat_720p_Generic(b *testing.B) {
	c := h264.DefaultEncoderConfig()
	c.Width = 1280
	c.Height = 720
	c.GOPSize = 600
	e, err := New(c)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := pattern.Frame(c.Width, c.Height, 1)
	for comp := range f.Planes {
		for i := range f.Planes[comp].Data {
			f.Planes[comp].Data[i] = 129
		}
	}
	defer f.Release()
	if err = e.SendFrame(f); err != nil {
		b.Fatal(err)
	}
	e.ReceivePacket()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = e.SendFrame(f); err != nil {
			b.Fatal(err)
		}
		if _, err = e.ReceivePacket(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncoderABR_720p_Generic(b *testing.B) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height, c.GOPSize = 1280, 720, 30
	c.RateControl, c.Bitrate = h264.RateControlABR, 2_000_000
	benchmarkRateControl(b, c)
}

func BenchmarkEncoderCBR_720p_Generic(b *testing.B) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height, c.GOPSize = 1280, 720, 30
	c.RateControl, c.Bitrate, c.VBVBufferSize = h264.RateControlCBR, 2_000_000, 2_000_000
	benchmarkRateControl(b, c)
}

func benchmarkRateControl(b *testing.B, c h264.EncoderConfig) {
	e, err := New(c)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := pattern.Frame(c.Width, c.Height, 1)
	defer f.Release()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = e.SendFrame(f); err != nil {
			b.Fatal(err)
		}
		if _, err = e.ReceivePacket(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncoderCABAC_720p_Generic(b *testing.B) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 1280, 720
	c.Profile = h264.ProfileMain
	benchmarkRateControl(b, c)
}

func BenchmarkEncoderB_720p_Generic(b *testing.B) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 1280, 720
	c.Profile = h264.ProfileHigh
	c.MaxBFrames = 2
	e, err := New(c, WithTransform8x8())
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := pattern.Frame(1280, 720, 1)
	defer f.Release()
	b.ReportAllocs()
	b.SetBytes(int64(4 * 1280 * 720 * 3 / 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = e.Reset(); err != nil {
			b.Fatal(err)
		}
		// 一个op必须实际包含IDR/P/B/B与尾部排空，不能用仅首帧的1x结果
		// 冒充B编码基准。每帧时间由ns/op除4得到。
		for frame := 0; frame < 4; frame++ {
			f.PTS = int64(frame)
			f.Duration = 1
			if err = e.SendFrame(f); err != nil {
				b.Fatal(err)
			}
			for {
				_, err = e.ReceivePacket()
				if errors.Is(err, h264.ErrAgain) {
					break
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		}
		if err = e.Flush(); err != nil {
			b.Fatal(err)
		}
		for {
			_, err = e.ReceivePacket()
			if errors.Is(err, h264.ErrEOF) {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
