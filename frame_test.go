package h264

import "testing"

// 格式元数据是后续所有尺寸计算的基础，必须逐项锁定：
// 位深、每采样字节、色度抽样因子任何一项错了，都会在后面表现为
// "画面轻微错位"或"内存越界"，极难定位。
func TestPixelFormatMetadata(t *testing.T) {
	tests := []struct {
		format    PixelFormat
		name      string
		depth     int
		bps       int
		subW      int
		subH      int
	}{
		{YUV420P, "yuv420p", 8, 1, 2, 2},
		{YUV422P, "yuv422p", 8, 1, 2, 1},
		{YUV444P, "yuv444p", 8, 1, 1, 1},
		{YUV420P10LE, "yuv420p10le", 10, 2, 2, 2},
		{YUV422P10LE, "yuv422p10le", 10, 2, 2, 1},
		{YUV444P10LE, "yuv444p10le", 10, 2, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.format.Valid() {
				t.Fatal("格式应当合法")
			}
			if got := tc.format.String(); got != tc.name {
				t.Fatalf("String() = %q", got)
			}
			if got := tc.format.BitDepth(); got != tc.depth {
				t.Fatalf("BitDepth() = %d，期望 %d", got, tc.depth)
			}
			if got := tc.format.BytesPerSample(); got != tc.bps {
				t.Fatalf("BytesPerSample() = %d，期望 %d", got, tc.bps)
			}
			subW, subH := tc.format.ChromaSubsampling()
			if subW != tc.subW || subH != tc.subH {
				t.Fatalf("ChromaSubsampling() = %d,%d，期望 %d,%d", subW, subH, tc.subW, tc.subH)
			}
		})
	}
}

func TestPixelFormatInvalid(t *testing.T) {
	bad := PixelFormat(pixelFormatCount)
	if bad.Valid() {
		t.Fatal("越界枚举不应合法")
	}
	if got := bad.String(); got != "unknown" {
		t.Fatalf("String() = %q，期望 unknown", got)
	}
	// 对非法格式，几何查询必须安全返回零值：0 会让后续乘法得到 0，
	// 从而在分配前就被容量检查拒绝，而不是静默分配出错误大小。
	if w, h := bad.PlaneDimensions(1, 640, 480); w != 0 || h != 0 {
		t.Fatalf("非法格式的平面尺寸 = %dx%d，期望 0x0", w, h)
	}
	if got := bad.RowBytes(1, 640); got != 0 {
		t.Fatalf("非法格式的行字节 = %d，期望 0", got)
	}
}

// 平面几何必须覆盖"标准尺寸"和"裁剪后的奇数尺寸"两类：
// 奇数宽高来自 SPS 裁剪窗口，编码器不做假设，解码器也不能假设。
func TestPixelFormatPlaneDimensions(t *testing.T) {
	tests := []struct {
		format PixelFormat
		width  int
		height int
		plane  int
		wantW  int
		wantH  int
	}{
		{YUV420P, 352, 288, 0, 352, 288},
		{YUV420P, 352, 288, 1, 176, 144},
		{YUV420P, 352, 288, 2, 176, 144},
		{YUV420P, 353, 289, 1, 177, 145},
		{YUV420P, 1, 1, 1, 1, 1},
		{YUV422P, 640, 480, 1, 320, 480},
		{YUV444P, 640, 480, 2, 640, 480},
		{YUV420P10LE, 1920, 1080, 1, 960, 540},
	}
	for _, tc := range tests {
		w, h := tc.format.PlaneDimensions(tc.plane, tc.width, tc.height)
		if w != tc.wantW || h != tc.wantH {
			t.Fatalf("%s plane %d of %dx%d = %dx%d，期望 %dx%d",
				tc.format, tc.plane, tc.width, tc.height, w, h, tc.wantW, tc.wantH)
		}
	}

	// 越界平面编号必须返回 0,0，不能 panic，也不能返回亮度尺寸。
	for _, plane := range []int{-1, 3, 99} {
		if w, h := YUV420P.PlaneDimensions(plane, 64, 64); w != 0 || h != 0 {
			t.Fatalf("plane %d 返回 %dx%d，期望 0x0", plane, w, h)
		}
	}
}

func TestPixelFormatRowBytes(t *testing.T) {
	tests := []struct {
		format PixelFormat
		plane  int
		width  int
		want   int
	}{
		{YUV420P, 0, 352, 352},
		{YUV420P, 1, 352, 176},
		{YUV422P, 1, 352, 176},
		{YUV444P, 2, 352, 352},
		{YUV420P10LE, 0, 352, 704},
		{YUV420P10LE, 1, 352, 352},
	}
	for _, tc := range tests {
		if got := tc.format.RowBytes(tc.plane, tc.width); got != tc.want {
			t.Fatalf("%s plane %d width %d RowBytes = %d，期望 %d",
				tc.format, tc.plane, tc.width, got, tc.want)
		}
	}
}

// newPackedFrame 构造一张"行紧密排列"的合法帧，测试里反复用到。
func newPackedFrame(format PixelFormat, width, height int) *Frame {
	f := &Frame{Width: width, Height: height, Format: format}
	for i := 0; i < 3; i++ {
		_, rows := format.PlaneDimensions(i, width, height)
		rowBytes := format.RowBytes(i, width)
		f.Planes[i] = Plane{Data: make([]byte, rowBytes*rows), Stride: rowBytes}
	}
	return f
}

// Clone 必须是深拷贝：解码器输出的帧内存会被 DPB 复用，
// 共享底层数组会让调用方看到"隔几帧内容自己变了"的诡异现象。
func TestFrameCloneIsDeepCopy(t *testing.T) {
	src := newPackedFrame(YUV420P, 32, 16)
	for i := range src.Planes[0].Data {
		src.Planes[0].Data[i] = byte(i)
	}
	src.PTS = 9000
	src.Duration = 3003
	src.KeyFrame = true

	dst := src.Clone()
	if dst == nil {
		t.Fatal("Clone 返回 nil")
	}
	if dst.Width != src.Width || dst.Height != src.Height || dst.Format != src.Format {
		t.Fatal("几何/格式字段未复制")
	}
	if dst.PTS != src.PTS || dst.Duration != src.Duration || !dst.KeyFrame {
		t.Fatal("时间戳或关键帧标志未复制")
	}
	for i := range src.Planes {
		if dst.Planes[i].Stride != src.Planes[i].Stride {
			t.Fatalf("plane %d stride 未保留", i)
		}
		if len(dst.Planes[i].Data) != len(src.Planes[i].Data) {
			t.Fatalf("plane %d 长度不一致", i)
		}
		if &dst.Planes[i].Data[0] == &src.Planes[i].Data[0] {
			t.Fatalf("plane %d 共享了底层数组", i)
		}
	}
	// 改源数据，克隆体必须不受影响。
	before := dst.Planes[0].Data[1]
	src.Planes[0].Data[1] ^= 0xFF
	if dst.Planes[0].Data[1] != before {
		t.Fatal("克隆体随源数据变化，说明仍是浅拷贝")
	}
}

func TestFrameCloneNil(t *testing.T) {
	var f *Frame
	if got := f.Clone(); got != nil {
		t.Fatalf("nil Frame 的 Clone = %v，期望 nil", got)
	}
}

// Release 必须幂等，且释放后帧不可再通过 Validate。
func TestFrameReleaseIsIdempotent(t *testing.T) {
	f := newPackedFrame(YUV420P, 64, 64)
	calls := 0
	f.onRelease = func() { calls++ }

	f.Release()
	f.Release()

	if calls != 1 {
		t.Fatalf("回收回调执行了 %d 次，期望 1 次", calls)
	}
	for i := range f.Planes {
		if f.Planes[i].Data != nil {
			t.Fatalf("plane %d 释放后仍持有内存", i)
		}
	}
	if err := f.Validate(); !isInvalidData(err) {
		t.Fatalf("释放后 Validate = %v，期望 ErrInvalidData", err)
	}

	var nilFrame *Frame
	nilFrame.Release() // 不得 panic
}

// Clone 不拷贝行尾填充：填充区在 SIMD 里只是越界读的安全边界，
// 不属于有效像素。保留填充值会让人误以为它可以被依赖（实际上它随时
// 可能被下一次重建覆盖），因此明确规定为“零填充 + 有效行完整拷贝”。
func TestFrameCloneZeroesPadding(t *testing.T) {
	f := &Frame{Width: 16, Height: 8, Format: YUV420P}
	for i := 0; i < 3; i++ {
		_, rows := f.Format.PlaneDimensions(i, f.Width, f.Height)
		rowBytes := f.Format.RowBytes(i, f.Width)
		stride := rowBytes + 8
		f.Planes[i] = Plane{Data: make([]byte, stride*rows), Stride: stride}
		for j := range f.Planes[i].Data {
			f.Planes[i].Data[j] = 0xAA
		}
	}

	dst := f.Clone()
	if dst.Planes[0].Stride != f.Planes[0].Stride {
		t.Fatal("stride 未保留")
	}

	rowBytes := f.Format.RowBytes(0, f.Width)
	for y := 0; y < f.Height; y++ {
		off := y * dst.Planes[0].Stride
		for x := 0; x < rowBytes; x++ {
			if dst.Planes[0].Data[off+x] != 0xAA {
				t.Fatalf("有效像素未拷贝: row %d col %d", y, x)
			}
		}
		for x := rowBytes; x < dst.Planes[0].Stride; x++ {
			if dst.Planes[0].Data[off+x] != 0 {
				t.Fatalf("填充区未清零: row %d col %d", y, x)
			}
		}
	}
}
