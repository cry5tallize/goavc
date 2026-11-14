package h264

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestEncoderConfig_High10SyntaxQPRange(t *testing.T) {
	for _, qp := range []int{-13, -12, -1, 0, 51, 52} {
		c := validEncoderConfig()
		c.Profile, c.PixelFormat, c.QP = ProfileHigh10, YUV420P10LE, qp
		err := c.Validate()
		if qp < -12 || qp > 51 {
			if !errors.Is(err, ErrInvalidData) {
				t.Fatalf("QP %d: %v", qp, err)
			}
		} else if err != nil {
			t.Fatalf("QP %d: %v", qp, err)
		}
	}
}

func TestFrameValidate_High10ValuesExcludePadding(t *testing.T) {
	f := newPackedFrame(YUV420P10LE, 3, 3)
	for comp := range f.Planes {
		w, h := f.Format.PlaneDimensions(comp, f.Width, f.Height)
		stride := w*2 + 3
		data := make([]byte, (h-1)*stride+w*2)
		for i := range data {
			data[i] = 255
		}
		for y := range h {
			for x := range w {
				binary.LittleEndian.PutUint16(data[y*stride+2*x:], 1023)
			}
		}
		f.Planes[comp] = Plane{Data: data, Stride: stride}
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	last := &f.Planes[2]
	binary.LittleEndian.PutUint16(last.Data[len(last.Data)-2:], 1024)
	if err := f.Validate(); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("out-of-range high bits accepted: %v", err)
	}
}
