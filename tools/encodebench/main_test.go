package main

import (
	"bytes"
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/encoder"
	"io"
	"testing"
)

func TestMeasureInput_RawFramesAndTruncation(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 16
	c.Height = 16
	raw := bytes.Repeat([]byte{128}, 16*16*3)
	r, _, err := measureInput(c, 2, 1, false, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if r.InputSHA256 != r.ReconstructionSHA256 || r.PSNR != [3]*float64{} {
		t.Fatal(r)
	}
	if _, _, err = measureInput(c, 1, 1, false, bytes.NewReader(raw[:383])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}

func TestPSNR_Analytic(t *testing.T) {
	if psnr(0, 16) != nil {
		t.Fatal("lossless must be infinity")
	}
	v := psnr(255*255*16, 16)
	if v == nil || *v != 0 {
		t.Fatal(v)
	}
}
func TestMeasure_PCMIsLossless(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 16
	c.Height = 16
	r, b, err := measureInput(c, 2, 1, true, bytes.NewReader(bytes.Repeat([]byte{128}, 16*16*3)))
	if err != nil {
		t.Fatal(err)
	}
	if r.InputSHA256 != r.ReconstructionSHA256 || r.Bytes != len(b) || r.PSNR != [3]*float64{} {
		t.Fatalf("%+v", r)
	}
}

func TestMeasure_DelayedBReconstructionAndVerification(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 32, 32
	c.Profile = h264.ProfileHigh
	c.MaxBFrames = 2
	c.GOPSize = 7
	raw := bytes.Repeat([]byte{128}, 32*32*3/2*12)
	var recon bytes.Buffer
	r, stream, err := measureConfigured(c, 12, 2, false, bytes.NewReader(raw), []encoder.Option{encoder.WithTransform8x8(), encoder.WithLookahead(2)}, &recon, true)
	if err != nil {
		t.Fatal(err)
	}
	if !r.DecoderVerified || !bytes.Equal(recon.Bytes(), raw) || r.InputSHA256 != r.ReconstructionSHA256 || r.Bytes != len(stream) || len(r.QPSequence) != 12 {
		t.Fatalf("延迟测量错误：%+v", r)
	}
	if r.PictureCounts["B"] == 0 || r.PictureCounts["IDR"] == 0 {
		t.Fatal("测量报告没有实际帧型统计", r.PictureCounts)
	}
	if len(r.FrameBits) != 12 || len(r.WindowBitrateKbps) != 1 || r.WindowBitrateKbps[0] != r.BitrateKbps {
		t.Fatal("窗口码率统计错误", r.WindowBitrateKbps)
	}
}

func TestMeasure_FractionalFrameRate(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 16, 16
	c.FPSNum, c.FPSDen = 30000, 1001
	r, _, err := measure(c, 2, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	want := float64(r.Bytes*8) * 30000 / (2 * 1001 * 1000)
	if r.BitrateKbps != want {
		t.Fatalf("分数FPS码率%f，期望%f", r.BitrateKbps, want)
	}
	if r.IDRInterval != c.GOPSize {
		t.Fatal("报告未应用默认IDR配置")
	}
}

func TestMeasure_VBVTraceAndDecodeOrder(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 32, 32
	c.Profile, c.MaxBFrames = h264.ProfileHigh, 2
	c.RateControl, c.Bitrate, c.VBVBufferSize = h264.RateControlVBV, 9000, 9000
	c.FPSNum, c.FPSDen = 30000, 1001
	const frames = 40
	raw := bytes.Repeat([]byte{128}, 32*32*3/2*frames)
	r, _, err := measureConfigured(c, frames, 2, false, bytes.NewReader(raw), nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.VBVFullnessSequence) != frames || len(r.FrameBits) != frames || r.VBVBufferBits != 9000 || r.FPSDen != 1001 {
		t.Fatal("逐AU缓冲轨迹不完整", r)
	}
	fullness, remainder, total := int64(4500), int64(0), int64(0)
	seen := map[int64]bool{}
	for i, bits := range r.FrameBits {
		numerator := int64(c.Bitrate*c.FPSDen) + remainder
		fullness += bits - numerator/int64(c.FPSNum)
		remainder = numerator % int64(c.FPSNum)
		total += bits
		if fullness != r.VBVFullnessSequence[i] || fullness < 0 || fullness > 9000 {
			t.Fatalf("第%d个AU缓冲与独立充放水解析解不符：%d/%d", i, r.VBVFullnessSequence[i], fullness)
		}
		if seen[r.PacketPTS[i]] || r.PacketPTS[i] < 0 || r.PacketPTS[i] >= frames || i > 0 && r.PacketDTS[i] <= r.PacketDTS[i-1] {
			t.Fatal("实际包时间戳不满足重排契约", r.PacketPTS, r.PacketDTS)
		}
		seen[r.PacketPTS[i]] = true
	}
	if total != int64(r.Bytes)*8 || !r.DecoderVerified {
		t.Fatal("逐AU总码量或解码验证标记错误", r)
	}
}

type shortReconWriter struct{}

func (shortReconWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestMeasure_ReconstructionShortWrite(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 16, 16
	_, _, err := measureConfigured(c, 1, 1, false, nil, nil, shortReconWriter{}, false)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("重建写出短写未报告", err)
	}
}
