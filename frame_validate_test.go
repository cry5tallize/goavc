package h264

import (
	"math"
	"testing"
)

func TestFrameValidate(t *testing.T) {
	tests := []struct {
		name    string
		frame   func() *Frame
		wantErr func(error) bool
	}{
		{
			name:    "nil frame",
			frame:   func() *Frame { return nil },
			wantErr: isInvalidData,
		},
		{
			name:    "packed 420",
			frame:   func() *Frame { return newPackedFrame(YUV420P, 352, 288) },
			wantErr: nil,
		},
		{
			name: "padded stride",
			frame: func() *Frame {
				f := &Frame{Width: 352, Height: 288, Format: YUV420P}
				for i := 0; i < 3; i++ {
					_, rows := f.Format.PlaneDimensions(i, f.Width, f.Height)
					rowBytes := f.Format.RowBytes(i, f.Width)
					stride := rowBytes + 32 // 模拟 SIMD 对齐填充
					f.Planes[i] = Plane{Data: make([]byte, stride*(rows-1)+rowBytes), Stride: stride}
				}
				return f
			},
			wantErr: nil,
		},
		{
			name: "odd display dimensions after crop",
			frame: func() *Frame {
				// 641×361 是裁剪后的真实显示尺寸，色度平面为 321×181。
				return newPackedFrame(YUV420P, 641, 361)
			},
			wantErr: nil,
		},
		{
			name:    "10 bit packed 420",
			frame:   func() *Frame { return newPackedFrame(YUV420P10LE, 1920, 1080) },
			wantErr: nil,
		},
		{
			name:    "444 packed",
			frame:   func() *Frame { return newPackedFrame(YUV444P, 128, 96) },
			wantErr: nil,
		},
		{
			name: "zero width",
			frame: func() *Frame {
				f := newPackedFrame(YUV420P, 64, 64)
				f.Width = 0
				return f
			},
			wantErr: isInvalidData,
		},
		{
			name: "negative height",
			frame: func() *Frame {
				f := newPackedFrame(YUV420P, 64, 64)
				f.Height = -16
				return f
			},
			wantErr: isInvalidData,
		},
		{
			name: "unknown format",
			frame: func() *Frame {
				f := newPackedFrame(YUV420P, 64, 64)
				f.Format = PixelFormat(200)
				return f
			},
			wantErr: isUnsupported,
		},
		{
			name: "plane without data",
			frame: func() *Frame {
				f := newPackedFrame(YUV420P, 64, 64)
				f.Planes[1] = Plane{}
				return f
			},
			wantErr: isInvalidData,
		},
		{
			name: "stride below row size",
			frame: func() *Frame {
				f := newPackedFrame(YUV420P, 64, 64)
				f.Planes[0].Stride = 32
				return f
			},
			wantErr: isInvalidData,
		},
		{
			name: "buffer one row short",
			frame: func() *Frame {
				f := newPackedFrame(YUV420P, 64, 64)
				p := &f.Planes[0]
				p.Data = p.Data[:len(p.Data)-p.Stride]
				return f
			},
			wantErr: isInvalidData,
		},
		{
			name: "buffer one byte short",
			frame: func() *Frame {
				f := newPackedFrame(YUV420P, 64, 64)
				p := &f.Planes[2]
				p.Data = p.Data[:len(p.Data)-1]
				return f
			},
			wantErr: isInvalidData,
		},
		{
			name: "dimensions that overflow multiplication",
			frame: func() *Frame {
				// 直接构造极端尺寸，不能依赖 newPackedFrame 分配（那会先 OOM）。
				return &Frame{
					Width:  math.MaxInt/2 + 1,
					Height: 2,
					Format: YUV420P,
					Planes: [3]Plane{{Data: []byte{0}, Stride: 1}},
				}
			},
			wantErr: isInvalidData,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.frame()
			err := f.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("期望合法，得到 %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("期望报错，得到 nil")
			}
			if !tc.wantErr(err) {
				t.Fatalf("错误分类不符: %v", err)
			}
		})
	}
}

// 尺寸乘法与加法必须可溢出检测，且对负数输入一律拒绝。
func TestSizeOverflowHelpers(t *testing.T) {
	if _, ok := mulOK(-1, 4); ok {
		t.Fatal("负数乘法应失败")
	}
	if _, ok := mulOK(4, -1); ok {
		t.Fatal("负数乘法应失败")
	}
	if v, ok := mulOK(0, math.MaxInt); !ok || v != 0 {
		t.Fatalf("mulOK(0, MaxInt) = %d,%v", v, ok)
	}
	if _, ok := mulOK(math.MaxInt/2+1, 2); ok {
		t.Fatal("乘法溢出未检测到")
	}
	if v, ok := mulOK(3, 4); !ok || v != 12 {
		t.Fatalf("mulOK(3,4) = %d,%v", v, ok)
	}

	if _, ok := addOK(-1, 1); ok {
		t.Fatal("负数加法应失败")
	}
	if _, ok := addOK(math.MaxInt, 1); ok {
		t.Fatal("加法溢出未检测到")
	}
	if v, ok := addOK(5, 7); !ok || v != 12 {
		t.Fatalf("addOK(5,7) = %d,%v", v, ok)
	}
}
