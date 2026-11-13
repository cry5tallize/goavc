package h264

import "fmt"

// validLevels 是 H.264 Annex A 中定义过的 level_idc 取值集合。
//
// 只做"是否为规范定义值"的检查，不做"该 level 是否足以支撑当前分辨率/帧率"
// 的推导检查：后者需要 MaxMBPS、MaxDPB 等完整约束表，属于编码器初始化阶段
// （M4）的责任，在这里实现会让配置层与码流层耦合。
var validLevels = map[int]struct{}{
	10: {}, 11: {}, 12: {}, 13: {},
	20: {}, 21: {}, 22: {},
	30: {}, 31: {}, 32: {},
	40: {}, 41: {}, 42: {},
	50: {}, 51: {}, 52: {},
	60: {}, 61: {}, 62: {},
}

// Validate 检查编码器配置是否合法且自洽。
//
// 调用顺序约定：SetDefaults → Validate → 构造编码器。Validate 不做补全，
// 因此对零值字段（FPS、GOPSize）会直接报错，这是有意的：
// 它能暴露"忘记调用 SetDefaults"的编程错误，而不是把它变成静默默认值。
//
// 错误分类：
//   - 结构非法（尺寸、QP、帧率越界）→ ErrInvalidData
//   - 规范不允许的组合（Baseline + B 帧、profile 与格式不匹配）→ ErrUnsupported
func (c EncoderConfig) Validate() error {
	if c.Width < MinFrameWidth || c.Width > MaxFrameWidth {
		return fmt.Errorf("%w: width %d outside [%d,%d]", ErrInvalidData, c.Width, MinFrameWidth, MaxFrameWidth)
	}
	if c.Height < MinFrameHeight || c.Height > MaxFrameHeight {
		return fmt.Errorf("%w: height %d outside [%d,%d]", ErrInvalidData, c.Height, MinFrameHeight, MaxFrameHeight)
	}
	if !c.PixelFormat.Valid() {
		return fmt.Errorf("%w: pixel format %d", ErrUnsupported, uint8(c.PixelFormat))
	}
	switch c.Profile {
	case ProfileBaseline, ProfileMain, ProfileHigh, ProfileHigh10, ProfileHigh422, ProfileHigh444:
	default:
		return fmt.Errorf("%w: profile %q", ErrUnsupported, c.Profile)
	}
	if err := validateProfileFormat(c.Profile, c.PixelFormat); err != nil {
		return err
	}
	if c.Level != LevelAuto {
		if _, ok := validLevels[c.Level]; !ok {
			return fmt.Errorf("%w: level_idc %d", ErrInvalidData, c.Level)
		}
	}
	if c.FPSNum <= 0 {
		return fmt.Errorf("%w: fps numerator %d must be positive", ErrInvalidData, c.FPSNum)
	}
	if c.FPSDen <= 0 {
		return fmt.Errorf("%w: fps denominator %d must be positive", ErrInvalidData, c.FPSDen)
	}
	if c.QP < MinQP || c.QP > MaxQP {
		return fmt.Errorf("%w: qp %d outside [%d,%d]", ErrInvalidData, c.QP, MinQP, MaxQP)
	}
	if c.GOPSize < 1 || c.GOPSize > maxGOPSize {
		return fmt.Errorf("%w: gop size %d outside [1,%d]", ErrInvalidData, c.GOPSize, maxGOPSize)
	}
	if c.IDRInterval < 0 {
		return fmt.Errorf("%w: idr interval %d must not be negative", ErrInvalidData, c.IDRInterval)
	}
	if c.MaxBFrames < 0 || c.MaxBFrames > maxBFrames {
		return fmt.Errorf("%w: max b frames %d outside [0,%d]", ErrInvalidData, c.MaxBFrames, maxBFrames)
	}
	if c.MaxBFrames >= c.GOPSize {
		return fmt.Errorf("%w: max b frames %d must be below gop size %d", ErrInvalidData, c.MaxBFrames, c.GOPSize)
	}
	// Baseline 规范只包含 I/P slice。允许 Baseline+B 会产出 FFmpeg 能解但
	// 严格解码器拒收的非法码流，因此在这里直接拒绝，而不是等到写码流时才发现。
	if c.MaxBFrames > 0 && c.Profile == ProfileBaseline {
		return fmt.Errorf("%w: baseline profile has no b slices", ErrUnsupported)
	}
	if !c.RateControl.Valid() {
		return fmt.Errorf("%w: rate control %d", ErrInvalidData, uint8(c.RateControl))
	}
	if c.Bitrate < 0 {
		return fmt.Errorf("%w: bitrate %d must not be negative", ErrInvalidData, c.Bitrate)
	}
	if c.MaxBitrate < 0 {
		return fmt.Errorf("%w: max bitrate %d must not be negative", ErrInvalidData, c.MaxBitrate)
	}
	if c.RateControl != RateControlCQP && c.Bitrate <= 0 {
		return fmt.Errorf("%w: %s rate control requires a positive bitrate", ErrInvalidData, c.RateControl)
	}
	if c.MaxBitrate > 0 && c.MaxBitrate < c.Bitrate {
		return fmt.Errorf("%w: max bitrate %d below target %d", ErrInvalidData, c.MaxBitrate, c.Bitrate)
	}
	if c.Threads < 0 {
		return fmt.Errorf("%w: threads %d must not be negative", ErrInvalidData, c.Threads)
	}
	return nil
}

// Validate 检查解码器配置是否合法。
//
// 这里的每个上限都会在解析 SPS 之后、分配缓冲之前被实际使用，
// 因此要求它们必须是正数（零值必须被 SetDefaults 补全）。
func (c DecoderConfig) Validate() error {
	if c.MaxWidth < MinFrameWidth || c.MaxWidth > MaxFrameWidth {
		return fmt.Errorf("%w: max width %d outside [%d,%d]", ErrInvalidData, c.MaxWidth, MinFrameWidth, MaxFrameWidth)
	}
	if c.MaxHeight < MinFrameHeight || c.MaxHeight > MaxFrameHeight {
		return fmt.Errorf("%w: max height %d outside [%d,%d]", ErrInvalidData, c.MaxHeight, MinFrameHeight, MaxFrameHeight)
	}
	if c.MaxFrameSize < 0 {
		return fmt.Errorf("%w: max frame size %d must not be negative", ErrInvalidData, c.MaxFrameSize)
	}
	if c.MaxNALSize < 0 {
		return fmt.Errorf("%w: max nal size %d must not be negative", ErrInvalidData, c.MaxNALSize)
	}
	if c.MaxSliceCount < 0 {
		return fmt.Errorf("%w: max slice count %d must not be negative", ErrInvalidData, c.MaxSliceCount)
	}
	if c.MaxDPBFrames < 0 {
		return fmt.Errorf("%w: max dpb frames %d must not be negative", ErrInvalidData, c.MaxDPBFrames)
	}
	if c.Threads < 0 {
		return fmt.Errorf("%w: threads %d must not be negative", ErrInvalidData, c.Threads)
	}
	if !c.OutputFormat.Valid() {
		return fmt.Errorf("%w: output pixel format %d", ErrUnsupported, uint8(c.OutputFormat))
	}
	return nil
}

// validateProfileFormat 检查 profile 与像素格式的组合是否符合 Annex A。
//
// 映射关系（第一版范围）：
//
//	baseline / main / high : 4:2:0，8 bit
//	high10                 : 4:2:0，10 bit
//	high422                : 4:2:0 或 4:2:2，8/10 bit
//	high444                : 任意（含 4:4:4）
//
// 提前拒绝的意义：profile 与 SPS 中的 profile_idc、chroma_format_idc 必须一致，
// 若允许"high10 + yuv420p"，编码器就只能二选一——要么写错 profile_idc（非法码流），
// 要么写错色度格式（解码器按 10 bit 读 8 bit 数据，必然花屏）。
func validateProfileFormat(profile string, format PixelFormat) error {
	depth := format.BitDepth()
	subW, subH := format.ChromaSubsampling()
	is420 := subW == 2 && subH == 2
	is422 := subW == 2 && subH == 1

	switch profile {
	case ProfileBaseline, ProfileMain, ProfileHigh:
		if !is420 || depth != 8 {
			return fmt.Errorf("%w: profile %s requires yuv420p 8-bit, got %s", ErrUnsupported, profile, format)
		}
	case ProfileHigh10:
		if !is420 || depth != 10 {
			return fmt.Errorf("%w: profile %s requires yuv420p 10-bit, got %s", ErrUnsupported, profile, format)
		}
	case ProfileHigh422:
		if !is420 && !is422 {
			return fmt.Errorf("%w: profile %s requires 4:2:0 or 4:2:2, got %s", ErrUnsupported, profile, format)
		}
	}
	return nil
}
