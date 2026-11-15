package benchmarks_test

import (
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

// newFrame 构造一张行紧密排列的帧，模拟解码器的标准输出。
func newFrame(format h264.PixelFormat, width, height int) *h264.Frame {
	f := &h264.Frame{Width: width, Height: height, Format: format}
	for i := 0; i < 3; i++ {
		_, rows := format.PlaneDimensions(i, width, height)
		rowBytes := format.RowBytes(i, width)
		f.Planes[i] = h264.Plane{Data: make([]byte, rowBytes*rows), Stride: rowBytes}
	}
	return f
}

// BenchmarkFrameValidate 是"帧结构校验"的基线。
//
// 该函数在解码输出、编码输入、流式管道的每个边界都会被调用，
// 因此它必须与帧大小成线性、且不能分配。若后续优化引入分配，
// 这里会立刻暴露。
func BenchmarkFrameValidate(b *testing.B) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"320x240", 320, 240},
		{"640x480", 640, 480},
		{"1280x720", 1280, 720},
		{"1920x1080", 1920, 1080},
	}
	for _, s := range sizes {
		b.Run(s.name, func(b *testing.B) {
			f := newFrame(h264.YUV420P, s.w, s.h)
			b.SetBytes(int64(len(f.Planes[0].Data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := f.Validate(); err != nil {
					b.Fatalf("Validate 失败: %v", err)
				}
			}
		})
	}
}

// BenchmarkFrameClone 记录"深拷贝一帧"的成本，作为后续缓冲池优化的对照基线。
//
// 现状是每次 Clone 都分配新缓冲；M8 引入池化后，Stream 路径应当能做到
// 零分配，而保留 Clone 作为"调用方要长期持有"时的显式选择。
func BenchmarkFrameClone(b *testing.B) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"640x480", 640, 480},
		{"1920x1080", 1920, 1080},
	} {
		b.Run(size.name, func(b *testing.B) {
			f := newFrame(h264.YUV420P, size.w, size.h)
			b.SetBytes(int64(len(f.Planes[0].Data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out := f.Clone()
				if out == nil {
					b.Fatal("Clone 返回 nil")
				}
			}
		})
	}
}

// BenchmarkFrameRelease 验证释放路径本身不分配内存——
// 它在流式解码中每帧都会执行一次。
func BenchmarkFrameRelease(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f := newFrame(h264.YUV420P, 640, 480)
		b.StartTimer()
		f.Release()
		b.StopTimer()
	}
}
