package main

import (
	h264 "github.com/goavc/h264"
	"testing"
)

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
	r, b, err := measure(c, 2, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.InputSHA256 != r.ReconstructionSHA256 || r.Bytes != len(b) || r.PSNR != [3]*float64{} {
		t.Fatalf("%+v", r)
	}
}
