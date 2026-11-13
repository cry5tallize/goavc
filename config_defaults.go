package h264

// 库内默认值集中定义，便于文档、测试与 CLI 保持一致。
const (
	defaultFPSNum        = 30
	defaultFPSDen        = 1
	defaultGOPSize       = 30
	defaultQP            = 26 // 0..51 区间的中点偏下，画质/码率的常用折中
	defaultMaxNALSize    = 8 << 20
	defaultMaxSliceCount = 64
	defaultMaxDPBFrames  = 16
)

// DefaultEncoderConfig 返回一份可直接修改的编码器配置模板。
//
// 注意 Width/Height 留空（0），Validate 会拒绝尺寸为零的配置；
// 这是刻意的：编码器没有"合理默认分辨率"，由调用方明确指定。
func DefaultEncoderConfig() EncoderConfig {
	return EncoderConfig{
		PixelFormat: YUV420P,
		Profile:     ProfileBaseline,
		Level:       LevelAuto,
		FPSNum:      defaultFPSNum,
		FPSDen:      defaultFPSDen,
		GOPSize:     defaultGOPSize,
		QP:          defaultQP,
		RateControl: RateControlCQP,
	}
}

// SetDefaults 补全 EncoderConfig 中"零值表示自动"的字段。
//
// 补全规则（只处理真正歧义的零值，绝不覆盖调用方显式设置的值）：
//   - Profile 为空 → baseline
//   - FPSNum/FPSDen 同时为 0 → 30/1；仅 FPSDen 为 0 → 1
//   - GOPSize 为 0 → 30；IDRInterval 为 0 → 跟随 GOPSize
//
// 明确不补全 QP：QP=0 是合法值（接近无损），若把 0 当成"未设置"会静默改变
// 调用方意图。需要默认值时请使用 DefaultEncoderConfig。
func (c *EncoderConfig) SetDefaults() {
	if c.Profile == "" {
		c.Profile = ProfileBaseline
	}
	if c.FPSNum == 0 && c.FPSDen == 0 {
		c.FPSNum = defaultFPSNum
		c.FPSDen = defaultFPSDen
	} else if c.FPSDen == 0 {
		c.FPSDen = 1
	}
	if c.GOPSize == 0 {
		c.GOPSize = defaultGOPSize
	}
	if c.IDRInterval == 0 {
		c.IDRInterval = c.GOPSize
	}
}

// DefaultDecoderConfig 返回一份可直接使用的解码器配置。
func DefaultDecoderConfig() DecoderConfig {
	c := DecoderConfig{OutputFormat: YUV420P}
	c.SetDefaults()
	return c
}

// SetDefaults 补全 DecoderConfig 的上限字段。
//
// 全部上限都必须有具体数值：留 0 会让"未配置"和"无限制"混为一谈，
// 而解码器面对的是不可信码流，无限制等于给攻击者一个内存放大器。
func (c *DecoderConfig) SetDefaults() {
	if c.MaxWidth == 0 {
		c.MaxWidth = MaxFrameWidth
	}
	if c.MaxHeight == 0 {
		c.MaxHeight = MaxFrameHeight
	}
	if c.MaxNALSize == 0 {
		c.MaxNALSize = defaultMaxNALSize
	}
	if c.MaxSliceCount == 0 {
		c.MaxSliceCount = defaultMaxSliceCount
	}
	if c.MaxDPBFrames == 0 {
		c.MaxDPBFrames = defaultMaxDPBFrames
	}
}
