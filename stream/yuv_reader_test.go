package stream

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
	"testing/iotest"

	h264 "github.com/cry5tallize/goavc"
)

func TestYUVReader_KnownPlanarBytesAndOwnership(t *testing.T) {
	for _, c := range []struct {
		format       h264.PixelFormat
		size, chroma int
	}{{h264.YUV420P, 17, 4}, {h264.YUV422P, 21, 6}, {h264.YUV444P, 27, 9}} {
		raw := make([]byte, c.size)
		for i := range raw {
			raw[i] = byte(i)
		}
		r, err := NewYUVReader(iotest.OneByteReader(bytes.NewReader(bytes.Repeat(raw, 2))), 3, 3, c.format)
		if err != nil {
			t.Fatal(err)
		}
		first, err := r.ReadFrame()
		if err != nil || first.Validate() != nil || !bytes.Equal(first.Planes[0].Data, raw[:9]) || !bytes.Equal(first.Planes[1].Data, raw[9:9+c.chroma]) || !bytes.Equal(first.Planes[2].Data, raw[9+c.chroma:]) {
			t.Fatal("已知3×3平面布局不符", c.format, err)
		}
		second, err := r.ReadFrame()
		if err != nil || second.PTS != 1 || first.PTS != 0 || first.Duration != 1 {
			t.Fatal("帧序号或分块读取错误", err)
		}
		second.Planes[0].Data[0] = 255
		if first.Planes[0].Data[0] != 0 || cap(first.Planes[0].Data) != 9 {
			t.Fatal("读帧共享缓冲或平面暴露额外容量")
		}
		first.Release()
		if second.Validate() != nil {
			t.Fatal("释放前帧影响后帧")
		}
		second.Release()
		for range 2 {
			if f, err := r.ReadFrame(); f != nil || !errors.Is(err, io.EOF) {
				t.Fatal("完整帧后EOF错误", err)
			}
		}
	}
}

func TestYUVReader_TruncationAtEveryOffset(t *testing.T) {
	for cut := 0; cut < 17; cut++ {
		r, _ := NewYUVReader(bytes.NewReader(make([]byte, cut)), 3, 3, h264.YUV420P)
		f, err := r.ReadFrame()
		if f != nil || err == nil {
			t.Fatal("截断返回了帧", cut)
		}
		if cut == 0 {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		} else if !errors.Is(err, h264.ErrInvalidData) || !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			t.Fatal("截断被误判为正常EOF", cut, err)
		}
		if _, again := r.ReadFrame(); again != err {
			t.Fatal("错误后继续消费了流", cut)
		}
	}
}

func TestYUVReader_ConstructorAndClockLimits(t *testing.T) {
	for _, width := range []int{0, -1, math.MaxInt} {
		_, err := NewYUVReader(bytes.NewReader(nil), width, 3, h264.YUV420P)
		want := h264.ErrInvalidData
		if width == math.MaxInt {
			want = h264.ErrResourceLimit
		}
		if !errors.Is(err, want) {
			t.Fatal("尺寸未在分配前拒绝", width, err)
		}
	}
	if _, err := NewYUVReader(nil, 3, 3, h264.YUV420P); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
	for _, format := range []h264.PixelFormat{255} {
		if _, err := NewYUVReader(bytes.NewReader(nil), 3, 3, format); !errors.Is(err, h264.ErrUnsupported) {
			t.Fatal("未拒绝未知格式", format, err)
		}
	}
	r, _ := NewYUVReader(bytes.NewReader([]byte{1}), 3, 3, h264.YUV420P)
	r.nextPTS = math.MaxInt64
	if _, err := r.ReadFrame(); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("时间戳溢出未拒绝", err)
	}
}

func TestYUVReader_PreservesIOErrors(t *testing.T) {
	for _, prefix := range [][]byte{nil, {1}} {
		src := io.MultiReader(bytes.NewReader(prefix), iotest.ErrReader(io.ErrClosedPipe))
		r, _ := NewYUVReader(src, 3, 3, h264.YUV420P)
		if f, err := r.ReadFrame(); f != nil || !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal("底层I/O错误被吞掉", err)
		}
	}
}
