package stream

import (
	"bytes"
	"errors"
	"io"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func paddedFrame(format h264.PixelFormat) *h264.Frame {
	f := &h264.Frame{Width: 3, Height: 3, Format: format}
	next := byte(0)
	for comp := range f.Planes {
		width, height := format.PlaneDimensions(comp, 3, 3)
		stride := width + comp + 2
		data := bytes.Repeat([]byte{0xee}, (height-1)*stride+width)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				data[y*stride+x] = next
				next++
			}
		}
		f.Planes[comp] = h264.Plane{Data: data, Stride: stride}
	}
	return f
}

func TestYUVWriter_KnownBytesExcludeStridePadding(t *testing.T) {
	for _, c := range []struct {
		format h264.PixelFormat
		size   int
	}{{h264.YUV420P, 17}, {h264.YUV422P, 21}, {h264.YUV444P, 27}} {
		f := paddedFrame(c.format)
		var out bytes.Buffer
		w, _ := NewYUVWriter(&out)
		if err := w.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
		want := make([]byte, c.size)
		for i := range want {
			want[i] = byte(i)
		}
		if !bytes.Equal(out.Bytes(), want) || f.Validate() != nil {
			t.Fatal("写入了行填充或改动输入", c.format, out.Bytes())
		}
		f.Release()
		if !bytes.Equal(out.Bytes(), want) {
			t.Fatal("输出保留了输入帧引用")
		}
	}
}

type badWriter struct {
	calls int
	err   error
}

func (w *badWriter) Write(p []byte) (int, error) {
	w.calls++
	return len(p) - 1, w.err
}

func TestYUVWriter_IOFailureIsSticky(t *testing.T) {
	f := paddedFrame(h264.YUV420P)
	defer f.Release()
	for _, want := range []error{io.ErrShortWrite, io.ErrClosedPipe} {
		sink := &badWriter{}
		if want != io.ErrShortWrite {
			sink.err = want
		}
		w, _ := NewYUVWriter(sink)
		err := w.WriteFrame(f)
		if !errors.Is(err, want) {
			t.Fatal("写出错误被吞掉", err)
		}
		if again := w.WriteFrame(f); again != err || sink.calls != 1 {
			t.Fatal("I/O失败后仍继续写流", again, sink.calls)
		}
	}
}

func TestYUVWriter_ValidationBeforeAnyOutput(t *testing.T) {
	var out bytes.Buffer
	w, _ := NewYUVWriter(&out)
	f := paddedFrame(h264.YUV420P)
	last := f.Planes[2]
	f.Planes[2].Data = nil
	if err := w.WriteFrame(f); !errors.Is(err, h264.ErrInvalidData) || out.Len() != 0 {
		t.Fatal("验证后面的平面之前已写出", err)
	}
	f.Planes[2] = last
	if err := w.WriteFrame(f); err != nil {
		t.Fatal("结构错误污染了Writer", err)
	}
	f.Release()
	if _, err := NewYUVWriter(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if err := w.WriteFrame(nil); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	if err := w.WriteFrame(&h264.Frame{Width: 3, Height: 3, Format: h264.YUV420P10LE}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("未拒绝缺失高位深平面", err)
	}
}

func TestYUVIO_RoundTripMultipleFrames(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV422P, h264.YUV444P} {
		var out bytes.Buffer
		w, _ := NewYUVWriter(&out)
		f := paddedFrame(format)
		if err := w.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
		f.Planes[0].Data[0] = 255
		if err := w.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
		f.Release()
		r, _ := NewYUVReader(&out, 3, 3, format)
		for frame := 0; frame < 2; frame++ {
			got, err := r.ReadFrame()
			if err != nil {
				t.Fatal(err)
			}
			next := byte(0)
			for _, plane := range got.Planes {
				for _, value := range plane.Data {
					want := next
					if frame == 1 && next == 0 {
						want = 255
					}
					if value != want {
						t.Fatal("多帧往返混入padding或丢失像素", format, frame, next)
					}
					next++
				}
			}
			got.Release()
		}
		if _, err := r.ReadFrame(); !errors.Is(err, io.EOF) {
			t.Fatal("排空后未返回EOF", err)
		}
	}
}
