package stream

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	h264 "github.com/cry5tallize/goavc"
)

func TestYUVIO_High10KnownLayout(t *testing.T) {
	for _, tc := range []struct {
		format          h264.PixelFormat
		samples, chroma int
	}{{h264.YUV420P10LE, 17, 4}, {h264.YUV422P10LE, 21, 6}, {h264.YUV444P10LE, 27, 9}} {
		raw := make([]byte, 2*tc.samples)
		for i := range tc.samples {
			binary.LittleEndian.PutUint16(raw[2*i:], uint16((i*61+255)%1024))
		}
		r, err := NewYUVReader(iotest.OneByteReader(bytes.NewReader(raw)), 3, 3, tc.format)
		if err != nil {
			t.Fatal(err)
		}
		f, err := r.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		if f.Planes[0].Stride != 6 || !bytes.Equal(f.Planes[0].Data, raw[:18]) || !bytes.Equal(f.Planes[1].Data, raw[18:18+2*tc.chroma]) || !bytes.Equal(f.Planes[2].Data, raw[18+2*tc.chroma:]) {
			t.Fatalf("%s: wrong LE plane layout", tc.format)
		}
		// 写端独立加入奇数字节stride和最小末行，不允许把填充写入输出。
		for comp := range f.Planes {
			w, h := tc.format.PlaneDimensions(comp, 3, 3)
			rowBytes, stride := 2*w, 2*w+3
			data := bytes.Repeat([]byte{255}, (h-1)*stride+rowBytes)
			for y := range h {
				copy(data[y*stride:y*stride+rowBytes], f.Planes[comp].Data[y*rowBytes:(y+1)*rowBytes])
			}
			f.Planes[comp] = h264.Plane{Data: data, Stride: stride}
		}
		var out bytes.Buffer
		w, _ := NewYUVWriter(&out)
		if err := w.WriteFrame(f); err != nil || !bytes.Equal(out.Bytes(), raw) {
			t.Fatalf("%s: padded write %v", tc.format, err)
		}
		f.Release()
		if _, err := r.ReadFrame(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		for cut := 1; cut < len(raw); cut++ {
			tr, _ := NewYUVReader(bytes.NewReader(raw[:cut]), 3, 3, tc.format)
			if frame, err := tr.ReadFrame(); frame != nil || !errors.Is(err, h264.ErrInvalidData) || !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("%s cut%d: %v", tc.format, cut, err)
			}
		}
		raw[len(raw)-1] = 4
		bad, _ := NewYUVReader(bytes.NewReader(raw), 3, 3, tc.format)
		if frame, err := bad.ReadFrame(); frame != nil || !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("invalid 10-bit sample accepted: %v", err)
		}
	}
}
