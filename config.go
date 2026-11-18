package h264

// RateControl 选择码率控制策略。
//
// 实现顺序遵循路线图：CQP → ABR → CBR → VBV。在 M0 阶段这些枚举只是配置契约，
// 具体策略由 encoder 包在 M4/M5 落地；未实现的策略必须在构造编码器时返回
// ErrUnsupported，而不是悄悄退化成 CQP（那会让码率不可预期）。
type RateControl uint8

const (
	// RateControlCQP 是恒定 QP：不控制码率，输出质量稳定，码率随内容波动。
	// 它是最容易验证的模式，因此也是第一个实现的模式。
	RateControlCQP RateControl = iota

	// RateControlABR 是平均码率：以 GOP 或更长窗口为粒度逼近目标码率。
	RateControlABR

	// RateControlCBR 是恒定码率：在 ABR 基础上引入缓冲模型平滑每帧比特数。
	RateControlCBR

	// RateControlVBV 在 CBR 之上施加视频缓冲校验器约束，保证不越界。
	RateControlVBV
)

const rateControlCount = 4

var rateControlNames = [rateControlCount]string{"cqp", "abr", "cbr", "vbv"}

// String 返回稳定的策略名，用于日志与错误信息。
func (r RateControl) String() string {
	if !r.Valid() {
		return "unknown"
	}
	return rateControlNames[r]
}

// Valid 判断策略值是否在支持集合内。
func (r RateControl) Valid() bool { return r < rateControlCount }

// Profile 用字符串表达 AVC profile。使用字符串而不是内部整数 idc，
// 是为了让配置对调用方自解释（"high" vs 100），同时避免把规范常量固化到公共 API。
const (
	ProfileBaseline = "baseline"
	ProfileMain     = "main"
	ProfileHigh     = "high"
	ProfileHigh10   = "high10"
	ProfileHigh422  = "high422"
	ProfileHigh444  = "high444"
)

// 帧尺寸与 QP 的通用边界。
//
// 这些是"结构性"边界，与具体 codec 配置无关：
//   - 小于 16 的尺寸连一个宏块都放不下；
//   - 8192 是 4K/8K 内容的实用上限，同时给 32 位平台的尺寸乘法留出余量。
//
// 单个实例的更严格上限仍由 EncoderConfig / DecoderConfig 承担。
const (
	MinFrameWidth  = 16
	MinFrameHeight = 16
	MaxFrameWidth  = 8192
	MaxFrameHeight = 8192

	MinQP = 0  // 8-bit语法QPY下限；10-bit按QpBdOffset允许到-12
	MaxQP = 51 // 语法QPY上限；10-bit有效QP'可到63，不可混用两者

	// LevelAuto 表示由编码器根据分辨率、帧率与码率自动推导 level_idc。
	LevelAuto = 0

	maxGOPSize = 600 // 单 GOP 的最大帧数，防止配置错误导致永不输出 IDR
	maxBFrames = 16  // H.264 连续 B 帧的实际上限
)

// EncoderConfig 是编码器配置。
//
// 生命周期约定：调用 NewEncoder 时先由 SetDefaults 补全零值字段，再执行 Validate。
// 因此直接使用结构体字面量时可以只填 Width/Height，其余留零。
// 例外是 QP：0 是合法 QP 值，无法与"未设置"区分，所以 QP 必须显式给出
// （或使用 DefaultEncoderConfig 获得 26）。
type EncoderConfig struct {
	Width       int
	Height      int
	PixelFormat PixelFormat

	// Profile 取值见 Profile* 常量。空字符串按 baseline 处理（SetDefaults 补齐）。
	Profile string

	// Level 是 level_idc（10 表示 1.0，31 表示 3.1，41 表示 4.1 …）。
	// LevelAuto 表示自动推导。
	Level int

	// FPSNum/FPSDen 是帧率的有理数表示（例如 30000/1001 表示 29.97）。
	// 用有理数而不是浮点，是为了让时间基换算与码率计算可精确复现。
	FPSNum int
	FPSDen int

	// GOPSize 是计划 GOP 的长度（以帧计）；当前无 B 帧时只用于 GOP 周期边界。
	GOPSize int

	// IDRInterval 是可选的独立 IDR 间隔；0 表示与 GOPSize 相同。
	IDRInterval int

	// MaxBFrames 是连续 B 帧上限；0 表示只用 I/P（Baseline 必须为 0）。
	MaxBFrames int

	// Bitrate 与 MaxBitrate 单位是 bit/s。CQP 模式下可为 0。
	Bitrate    int
	MaxBitrate int
	// VBVBufferSize 是 CBR/VBV 反馈缓冲上限，单位 bit；为 0 时使用 MaxBitrate 的一秒容量，
	// 若 MaxBitrate 也为 0 则使用 Bitrate 的一秒容量。
	VBVBufferSize int

	// QP 是基准语法QPY：8-bit为0..51，10-bit为-12..51。
	QP int

	RateControl RateControl

	// Threads 是并行度；0 表示自动（按 GOMAXPROCS 与切片数量推导）。
	Threads int
}

// DecoderConfig 是解码器配置。
//
// 所有 Max* 字段都是抵御不可信输入的资源上限：码流里解析出来的每个尺寸
// 都必须先与上限比较，再决定是否分配。0 表示使用库内默认值（见 SetDefaults）。
type DecoderConfig struct {
	// Threads 限制同一张图的 slice 并行度。0 按 GOMAXPROCS 和 slice
	// 数量自动选择，1 强制串行；实际并行度不超过 64。
	// 不满足并行条件的码流仍串行解码。
	Threads int

	// MaxWidth/MaxHeight 是允许的最大解码分辨率。
	MaxWidth  int
	MaxHeight int

	// MaxFrameSize 是单帧缓冲的字节上限；0 表示按分辨率与格式推导。
	MaxFrameSize int

	// OutputFormat 是期望的输出格式。零值 YUV420P 表示"不转换"，
	// 即按码流原生格式输出（M0 阶段只有 4:2:0 8 bit 会被真正支持）。
	OutputFormat PixelFormat

	// MaxNALSize 是单个 NAL Unit 的字节上限，防止超长 NAL 触发大分配。
	MaxNALSize int

	// MaxSliceCount 是单帧允许的 slice 数上限。
	MaxSliceCount int

	// MaxDPBFrames 是 DPB 中参考帧数量上限，与 level 的 max_dec_frame_buffering 取小。
	MaxDPBFrames int
}
