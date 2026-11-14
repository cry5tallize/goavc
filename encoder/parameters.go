package encoder

import (
	"fmt"
	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/syntax"
)

// Annex A 表 A-1，MaxMBPS/MaxFS/MaxBR（Baseline，kbit/s）。
var levels = [...]struct{ id, mbps, fs, br int }{
	{10, 1485, 99, 64}, {11, 3000, 396, 192}, {12, 6000, 396, 384}, {13, 11880, 396, 768},
	{20, 11880, 396, 2000}, {21, 19800, 792, 4000}, {22, 20250, 1620, 4000},
	{30, 40500, 1620, 10000}, {31, 108000, 3600, 14000}, {32, 216000, 5120, 20000},
	{40, 245760, 8192, 20000}, {41, 245760, 8192, 50000}, {42, 522240, 8704, 50000},
	{50, 589824, 22080, 135000}, {51, 983040, 36864, 240000}, {52, 2073600, 36864, 240000},
	{60, 4177920, 139264, 240000}, {61, 8355840, 139264, 480000}, {62, 16711680, 139264, 800000},
}

func (e *Encoder) parameters() error {
	c := e.cfg
	mw, mh := (c.Width+15)/16, (c.Height+15)/16
	fs := int64(mw * mh)
	subW, subH := c.PixelFormat.ChromaSubsampling()
	chromaIDC := uint32(3)
	if subW == 2 {
		chromaIDC = 2
		if subH == 2 {
			chromaIDC = 1
		}
	}
	peakRate := int64(max(c.Bitrate, c.MaxBitrate))
	planeSets := 1
	if e.separateColourPlane {
		planeSets = 3
	}
	selected := 0
	fallback := 0
	for _, l := range levels {
		if c.Level != 0 && c.Level != l.id {
			continue
		}
		if fs > int64(l.fs) || int64(mw*mw) > int64(l.fs)*8 || int64(mh*mh) > int64(l.fs)*8 || fs*int64(c.FPSNum) > int64(l.mbps)*int64(c.FPSDen) {
			continue
		}
		if c.MaxBFrames > 0 && 3*fs > levelDPBMbs(l.id) {
			continue
		}
		maxRate := e.levelBitrate(l.br)
		if peakRate <= maxRate {
			fallback = l.id
		}
		// 按真实RawMbBits与EBSP最坏扩展预算，不把高采样格式当420计费。
		bytesPerMB := ((e.rawMbBits()+7)/8 + 16*planeSets) * 3 / 2
		bitsPerFrame := (fs*int64(bytesPerMB) + int64(e.slices*planeSets)*64 + 256) * 8
		if bitsPerFrame*int64(c.FPSNum) > maxRate*int64(c.FPSDen) || peakRate > maxRate {
			continue
		}
		selected = l.id
		break
	}
	// CQP 不能在 New 时预知内容。Auto 的最坏预算超出最高级别时，仍选择
	// 满足几何/帧率的最高级别，并在发布每个 AU 前实施实际字节预算。
	// 这允许真实 1080p30；不能满足预算的帧返回资源错误，不发布非法码流。
	if selected == 0 && c.Level == 0 && !e.pcm {
		selected = fallback
	}
	if selected == 0 {
		return fmt.Errorf("encoder: dimensions/frame rate/bitrate exceed level %d: %w", c.Level, h264.ErrUnsupported)
	}
	cropW, cropH := subW, subH
	if e.separateColourPlane {
		cropW, cropH = 1, 1
	}
	e.sps = &syntax.SPS{ProfileIDC: 66, ConstraintSetFlags: [6]bool{true, true}, LevelIDC: uint8(selected), ChromaFormatIDC: chromaIDC, SeparateColourPlaneFlag: e.separateColourPlane, PicOrderCntType: 2, MaxNumRefFrames: 1, PicWidthInMbsMinus1: uint32(mw - 1), PicHeightInMapUnitsMinus1: uint32(mh - 1), FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true, FrameCroppingFlag: c.Width != mw*16 || c.Height != mh*16, FrameCropRightOffset: uint32((mw*16 - c.Width) / cropW), FrameCropBottomOffset: uint32((mh*16 - c.Height) / cropH), VUIParametersPresentFlag: true, VUI: &syntax.VUI{TimingInfoPresentFlag: true, NumUnitsInTick: uint32(c.FPSDen), TimeScale: uint32(c.FPSNum * 2), FixedFrameRateFlag: true, BitstreamRestrictionFlag: true, MotionVectorsOverPicBoundariesFlag: true, MaxDecFrameBuffering: 1}}
	// E.2.1：显式声明保守MV范围，不以结构零值错误地承诺仅零运动。
	e.sps.VUI.Log2MaxMVLengthHorizontal, e.sps.VUI.Log2MaxMVLengthVertical = 15, 15
	e.pps = &syntax.PPS{PicInitQPMinus26: int32(c.QP - 26), DeblockingFilterControlPresentFlag: true}
	if c.Profile == h264.ProfileMain {
		e.sps.ProfileIDC = 77
		e.sps.ConstraintSetFlags = [6]bool{false, true}
	}
	if c.Profile == h264.ProfileHigh {
		e.sps.ProfileIDC = 100
		e.sps.ConstraintSetFlags = [6]bool{}
	}
	if c.Profile == h264.ProfileHigh10 {
		e.sps.ProfileIDC = 110
		e.sps.ConstraintSetFlags = [6]bool{}
	}
	if c.Profile == h264.ProfileHigh422 {
		e.sps.ProfileIDC = 122
		e.sps.ConstraintSetFlags = [6]bool{}
	}
	if c.Profile == h264.ProfileHigh444 {
		e.sps.ProfileIDC = 244
		e.sps.ConstraintSetFlags = [6]bool{}
	}
	if c.PixelFormat.BitDepth() > 8 {
		e.sps.BitDepthLumaMinus8 = uint32(c.PixelFormat.BitDepth() - 8)
		e.sps.BitDepthChromaMinus8 = e.sps.BitDepthLumaMinus8
	}
	e.pps.EntropyCodingModeFlag = e.cabac
	e.pps.WeightedPredFlag = e.autoWeights || e.fixedWeights != nil
	e.pps.HasOptionalBlock = e.transform8 || e.scaling4 != nil
	e.pps.Transform8x8ModeFlag = e.transform8
	if c.MaxBFrames > 0 {
		e.sps.PicOrderCntType = 0
		e.sps.Log2MaxPicOrderCntLsbMinus4 = 12
		e.sps.MaxNumRefFrames = 2
		// 未来锚点后按显示顺序编码非参考B；最多1个待输出重排锚点，
		// 2个参考与1个当前图像足以容纳，不随连续B数量增长。
		e.sps.VUI.MaxNumReorderFrames = 1
		e.sps.VUI.MaxDecFrameBuffering = 3
	}
	e.scalingParameters()
	for _, l := range levels {
		if l.id == selected {
			e.maxAUBytes = int(e.levelBitrate(l.br) * int64(c.FPSDen) / (8 * int64(c.FPSNum)))
			break
		}
	}
	return nil
}

// Annex A表A-1的MaxDpbMbs；B流水线要求两参考与一个非参考输出图像预算。
func levelDPBMbs(level int) int64 {
	switch level {
	case 10:
		return 396
	case 11:
		return 900
	case 12, 13, 20:
		return 2376
	case 21:
		return 4752
	case 22, 30:
		return 8100
	case 31:
		return 18000
	case 32:
		return 20480
	case 40, 41:
		return 32768
	case 42:
		return 34816
	case 50:
		return 110400
	case 51, 52:
		return 184320
	default:
		return 696320
	}
}

// rawMbBits是整个图像宏块的原始位数；独立平面只改变slice组织，不减少AU预算。
func (e *Encoder) rawMbBits() int {
	w, h := e.cfg.PixelFormat.PlaneDimensions(1, 16, 16)
	return (256 + 2*w*h) * e.cfg.PixelFormat.BitDepth()
}

// Annex A的VCL码率因子以Baseline/Main的1000为基准。
func (e *Encoder) levelBitrate(baseKbps int) int64 {
	factor := int64(1000)
	switch e.cfg.Profile {
	case h264.ProfileHigh:
		factor = 1250
	case h264.ProfileHigh10:
		factor = 3000
	case h264.ProfileHigh422, h264.ProfileHigh444:
		factor = 4000
	}
	return int64(baseKbps) * factor
}
