package h264

// PixelFormat 标识 Frame 在内存中的采样布局：平面顺序固定为 Y、U、V，
// 采样精度与色度抽样由枚举值唯一确定。
//
// 为什么内部只允许原生 YUV：H.264 的预测、变换、去块滤波全部在 YUV 域定义，
// 任何 RGB 往返都会引入不可逆的舍入误差，导致与 FFmpeg Oracle 的逐像素比对
// 失去意义。需要 RGB 的调用方请在库外自行转换。
type PixelFormat uint8

const (
	// YUV420P 是平面 4:2:0、每采样 8 bit。
	// 亮度 W×H，色度均为 ceil(W/2)×ceil(H/2)。
	YUV420P PixelFormat = iota

	// YUV422P 是平面 4:2:2、每采样 8 bit。
	// 亮度 W×H，色度均为 ceil(W/2)×H（水平减半，垂直不变）。
	YUV422P

	// YUV444P 是平面 4:4:4、每采样 8 bit。
	// 三个平面均为 W×H。
	YUV444P

	// YUV420P10LE 是平面 4:2:0、每采样 10 bit，按小端存放在 16 bit 字中。
	//
	// 高位深必须按"有效位深"而不是"存储字节数"参与裁剪与量化：
	// 例如 10 bit 样本取值范围是 0..1023，而不是 0..65535。
	YUV420P10LE

	// YUV422P10LE 是平面 4:2:2、每采样 10 bit，小端 16 bit 存储。
	YUV422P10LE

	// YUV444P10LE 是平面 4:4:4、每采样 10 bit，小端 16 bit 存储。
	YUV444P10LE
)

// pixelFormatCount 与上面的常量数量保持一致，是 Valid 的判定上界。
// 新增格式时必须同步：常量、pixelFormatNames、本常量三者缺一不可。
const pixelFormatCount = 6

// pixelFormatNames 用于日志与测试断言，命名与 ffmpeg 的 -pix_fmt 参数一致，
// 这样差分测试脚本可以直接复用同一个字符串。
var pixelFormatNames = [pixelFormatCount]string{
	"yuv420p",
	"yuv422p",
	"yuv444p",
	"yuv420p10le",
	"yuv422p10le",
	"yuv444p10le",
}

// String 返回与 ffmpeg -pix_fmt 兼容的格式名。
func (f PixelFormat) String() string {
	if !f.Valid() {
		return "unknown"
	}
	return pixelFormatNames[f]
}

// Valid 判断格式值是否在支持集合内。
//
// 该函数是所有格式相关代码的第一道闸门：Validate、配置校验、格式转换都必须先调用它，
// 避免用越界的枚举值去索引采样表。
func (f PixelFormat) Valid() bool { return f < pixelFormatCount }

// BitDepth 返回有效位深（8 或 10）。
func (f PixelFormat) BitDepth() int {
	switch f {
	case YUV420P10LE, YUV422P10LE, YUV444P10LE:
		return 10
	default:
		return 8
	}
}

// BytesPerSample 返回单个采样的存储字节数。
//
// 注意：它返回的是 1 或 2（存储宽度），不是位深。计算 plane 容量时用本方法，
// 计算取值范围与 clip 边界时用 BitDepth。
func (f PixelFormat) BytesPerSample() int {
	if f.BitDepth() > 8 {
		return 2
	}
	return 1
}

// ChromaSubsampling 返回色度的水平、垂直抽样因子（4:2:0 → 2,2；4:2:2 → 2,1；4:4:4 → 1,1）。
//
// 对非法格式返回 0,0，调用方必须先经 Valid 判断，不能拿 0 直接做除法。
func (f PixelFormat) ChromaSubsampling() (subW, subH int) {
	switch f {
	case YUV420P, YUV420P10LE:
		return 2, 2
	case YUV422P, YUV422P10LE:
		return 2, 1
	case YUV444P, YUV444P10LE:
		return 1, 1
	default:
		return 0, 0
	}
}

// PlaneDimensions 返回指定平面的有效采样宽高（不含 stride 填充）。
//
// plane 取值：0 = Y，1 = U，2 = V；其余返回 0,0。
//
// 这里一律用向上取整（ceil）而不是假设偶数：虽然 H.264 以宏块为单位编码，
// 但输出帧会按 SPS 的裁剪窗口裁到显示尺寸，显示宽高完全可能是奇数
//（例如 641×361）。用偶数假设会让这类帧的色度平面少一行/一列。
func (f PixelFormat) PlaneDimensions(plane, width, height int) (w, h int) {
	if plane < 0 || plane > 2 || !f.Valid() || width <= 0 || height <= 0 {
		return 0, 0
	}
	if plane == 0 {
		return width, height
	}
	subW, subH := f.ChromaSubsampling()
	if subW == 0 || subH == 0 {
		return 0, 0
	}
	return (width + subW - 1) / subW, (height + subH - 1) / subH
}

// RowBytes 返回指定平面一行的有效字节数（有效采样数 × 每采样字节数）。
//
// 注意它是"有效字节"而不是"stride"：stride 由调用方或内部分配器决定，
// 可以更大（用于对齐或 SIMD 越界读的安全边界）。
func (f PixelFormat) RowBytes(plane, width int) int {
	w, _ := f.PlaneDimensions(plane, width, 1)
	return w * f.BytesPerSample()
}

// Plane 是单个颜色平面的视图。
//
// 字段语义：
//   - Data 是整个平面缓冲；长度至少为 (rows-1)*Stride + RowBytes，
//     最后一行不要求补齐（因此不能用 Stride*rows 作为长度下限）。
//   - Stride 是一行的字节跨度，必须 ≥ 该平面的 RowBytes。
//
// 所有权：Data 可能来自 Frame 缓冲池。持有 Plane 的同时不得越过所属 Frame
// 的生命周期；`Frame.Release` 之后 Data 可能被池回收并复用。
type Plane struct {
	Data   []byte
	Stride int
}

// Frame 表示一张未压缩图像。
//
// 尺寸语义：Width/Height 是"有效显示尺寸"，不是编码时的宏块对齐尺寸。
// 编码器内部会按 16 对齐补齐（不足部分用边缘复制填充），解码器输出前会按
// SPS 裁剪窗口裁到显示尺寸。这样上层永远只看到真实画面，不需要自己处理 Crop。
//
// 所有权：Frame 可能由解码器缓冲池分配。调用方用完后必须调用 Release，
// 否则该缓冲无法回到池中，长时间流式处理会出现内存增长（GC 最终会回收，
// 但违背了零分配的目标）。Clone 出来的 Frame 持有独立内存，不参与池管理。
type Frame struct {
	Width  int
	Height int
	Format PixelFormat
	Planes [3]Plane

	PTS      int64
	Duration int64
	KeyFrame bool

	// onRelease 由缓冲池在分配时注入；为 nil 表示这是普通（非池化）帧。
	// 用小写字段是为了不污染公共 API，同时保证 Release 可以精确归还。
	onRelease func()
}

// Clone 返回一张深拷贝的 Frame。
//
// 用途：解码器输出帧来自内部 DPB，DPB 很快会复用它；调用方若要长期持有，
// 必须 Clone。Clone 保留原始 Stride（这样后续 SIMD 处理仍有相同的对齐假设），
// 但只拷贝有效行内的有效字节，不拷贝行尾填充。
//
// 对 nil 接收者返回 nil；对非法帧不做校验（内存不足等异常由调用方先行
// Validate 负责），拷贝过程本身对越界访问做了截断保护，不会 panic。
func (f *Frame) Clone() *Frame {
	if f == nil {
		return nil
	}
	out := &Frame{
		Width:    f.Width,
		Height:   f.Height,
		Format:   f.Format,
		PTS:      f.PTS,
		Duration: f.Duration,
		KeyFrame: f.KeyFrame,
	}
	if !f.Format.Valid() {
		return out
	}
	for i := 0; i < 3; i++ {
		src := f.Planes[i]
		if src.Data == nil {
			continue
		}
		_, rows := f.Format.PlaneDimensions(i, f.Width, f.Height)
		rowBytes := f.Format.RowBytes(i, f.Width)
		if src.Stride <= 0 || rowBytes <= 0 || rows <= 0 {
			continue
		}
		if rows > 1 && src.Stride > 0 && (rows-1) > len(src.Data)/src.Stride {
			// 缓冲不足以容纳声明的行数：按实际可用行截断，避免越界读取。
			rows = len(src.Data)/src.Stride + 1
		}
		buf := make([]byte, src.Stride*rows)
		for y := 0; y < rows; y++ {
			off := y * src.Stride
			if off >= len(src.Data) {
				break
			}
			n := rowBytes
			if off+n > len(src.Data) {
				n = len(src.Data) - off
			}
			copy(buf[off:off+n], src.Data[off:off+n])
		}
		out.Planes[i] = Plane{Data: buf, Stride: src.Stride}
	}
	return out
}

// Release 归还帧占用的内存并使 Frame 失效。
//
// 契约：
//   - 幂等：重复调用安全，第二次是空操作。
//   - 调用后 Plane.Data 全部为 nil，Validate 会返回 ErrInvalidData。
//   - 若帧来自缓冲池，内存立即回到池中，可能被下一帧覆盖；
//     仍在引用旧切片的代码属于使用已释放内存，本库不做防护。
func (f *Frame) Release() {
	if f == nil {
		return
	}
	fn := f.onRelease
	f.onRelease = nil
	f.Planes = [3]Plane{}
	if fn != nil {
		fn()
	}
}
