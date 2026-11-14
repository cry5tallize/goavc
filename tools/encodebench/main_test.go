package main

import (
	"bytes"
	"errors"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/encoder"
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
	if !bytes.Equal(recon.Bytes(), raw) || r.InputSHA256 != r.ReconstructionSHA256 || r.Bytes != len(stream) || len(r.QPSequence) != 12 {
		t.Fatalf("延迟测量错误：%+v", r)
	}
	if r.PictureCounts["B"] == 0 || r.PictureCounts["IDR"] == 0 {
		t.Fatal("测量报告没有实际帧型统计", r.PictureCounts)
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
