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
	selected := 0
	fallback := 0
	for _, l := range levels {
		if c.Level != 0 && c.Level != l.id {
			continue
		}
		if fs > int64(l.fs) || int64(mw*mw) > int64(l.fs)*8 || int64(mh*mh) > int64(l.fs)*8 || fs*int64(c.FPSNum) > int64(l.mbps)*int64(c.FPSDen) {
			continue
		}
		if int64(c.Bitrate) <= int64(l.br)*1000 {
			fallback = l.id
		}
		// I_PCM 大小决策上限 + EBSP 最坏扩展，保守按每 MB 600 字节计算。
		// 在 New 阶段即可拒绝不够的显式 Level；不依赖内容或 CQP 平均码率猜测。
		bitsPerFrame := (fs*600 + int64(e.slices)*64 + 256) * 8
		if bitsPerFrame*int64(c.FPSNum) > int64(l.br)*1000*int64(c.FPSDen) || int64(c.Bitrate) > int64(l.br)*1000 {
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
	e.sps = &syntax.SPS{ProfileIDC: 66, ConstraintSetFlags: [6]bool{true, true}, LevelIDC: uint8(selected), ChromaFormatIDC: 1, PicOrderCntType: 2, MaxNumRefFrames: 1, PicWidthInMbsMinus1: uint32(mw - 1), PicHeightInMapUnitsMinus1: uint32(mh - 1), FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true, FrameCroppingFlag: c.Width != mw*16 || c.Height != mh*16, FrameCropRightOffset: uint32((mw*16 - c.Width) / 2), FrameCropBottomOffset: uint32((mh*16 - c.Height) / 2), VUIParametersPresentFlag: true, VUI: &syntax.VUI{TimingInfoPresentFlag: true, NumUnitsInTick: uint32(c.FPSDen), TimeScale: uint32(c.FPSNum * 2), FixedFrameRateFlag: true, BitstreamRestrictionFlag: true, MotionVectorsOverPicBoundariesFlag: true, MaxDecFrameBuffering: 1}}
	e.pps = &syntax.PPS{PicInitQPMinus26: int32(c.QP - 26), DeblockingFilterControlPresentFlag: true}
	if c.Profile == h264.ProfileMain {
		e.sps.ProfileIDC = 77
		e.sps.ConstraintSetFlags = [6]bool{false, true}
	}
	if c.Profile == h264.ProfileHigh {
		e.sps.ProfileIDC = 100
		e.sps.ConstraintSetFlags = [6]bool{}
	}
	e.pps.EntropyCodingModeFlag = e.cabac
	e.pps.WeightedPredFlag = e.autoWeights || e.fixedWeights != nil
	for _, l := range levels {
		if l.id == selected {
			e.maxAUBytes = int(int64(l.br) * 1000 * int64(c.FPSDen) / (8 * int64(c.FPSNum)))
			break
		}
	}
	return nil
}
