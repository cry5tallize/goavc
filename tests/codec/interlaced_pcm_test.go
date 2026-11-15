package codec_test

import (
	"errors"
	"fmt"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
)

// 显式PCM必须逐采样保留场交织与高位深，不能因MBAFF对齐或场边界去块而改写输入。
func TestEncoder_InterlacedPCMThreeWayOracle(t *testing.T) {
	for _, format := range chromaEncoderFormats {
		for _, entropy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cabac%t", format, entropy), func(t *testing.T) {
				cfg := chromaEncoderConfig(format)
				cfg.Height = 36
				e, err := encoder.New(cfg, encoder.WithInterlaced(true), encoder.WithCABAC(entropy), encoder.WithPCM())
				if err != nil {
					t.Fatal(err)
				}
				defer e.Close()
				f := interlacedEncoderFrame(cfg, 0)
				defer f.Release()
				want := chromaEncoderPacked(t, f, cfg)
				if err := e.SendFrame(f); err != nil {
					t.Fatal(err)
				}
				p, err := e.ReceivePacket()
				if err != nil {
					t.Fatal(err)
				}
				recon, err := e.Reconstruction()
				if err != nil {
					t.Fatal(err)
				}
				chromaEncoderEqual(t, "MBAFF PCM本地", chromaEncoderPacked(t, recon, cfg), want, format.BytesPerSample())
				recon.Release()
				d, err := decoder.New(h264.DecoderConfig{})
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close()
				if err := d.SendPacket(p); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				got, err := d.ReceiveFrame()
				if err != nil {
					t.Fatal(err)
				}
				chromaEncoderEqual(t, "MBAFF PCM解码", chromaEncoderPacked(t, got, cfg), want, format.BytesPerSample())
				got.Release()
				if _, err := d.ReceiveFrame(); !errors.Is(err, h264.ErrEOF) {
					t.Fatalf("PCM produced an extra picture: %v", err)
				}
				chromaEncoderFFmpegCheck(t, p.Data, want, format)
			})
		}
	}
}
