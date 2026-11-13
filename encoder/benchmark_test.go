package encoder

import (
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/testutil/pattern"
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
