package decoder

import (
	"fmt"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/transform"
	"github.com/cry5tallize/goavc/syntax"
)

// siMacroblockType 只在分派内部使用，不与码流中的普通 I_NxN 混淆。
const siMacroblockType = -1

// checkSwitchingSupported 先检查损坏的量化参数，再拒绝尚无切换内核的组合。
// 普通 I 宏块即使位于 SP/SI slice 内，仍按 8.5 使用 QPY 重建。
func checkSwitchingSupported(sps *syntax.SPS, pps *syntax.PPS, hdr *syntax.SliceHeader) error {
	if hdr.Type != syntax.SliceTypeSP && hdr.Type != syntax.SliceTypeSI {
		return nil
	}
	if hdr.SliceQP < 0 || hdr.SliceQP > 51 || hdr.SliceQS < 0 || hdr.SliceQS > 51 {
		return fmt.Errorf("decoder: switching QP/QS %d/%d: %w", hdr.SliceQP, hdr.SliceQS, h264.ErrInvalidData)
	}
	if sps.ChromaFormatIDC != 1 || sps.BitDepthLumaMinus8 != 0 || sps.BitDepthChromaMinus8 != 0 || sps.SeparateColourPlaneFlag || sps.QPPPrimeYZeroTransformBypassFlag || sps.SeqScalingMatrixPresentFlag || pps.PicScalingMatrixPresentFlag || pps.Transform8x8ModeFlag || pps.EntropyCodingModeFlag || !sps.FrameMbsOnlyFlag || hdr.FieldPicFlag {
		return fmt.Errorf("decoder: switching requires progressive 8-bit 4:2:0 CAVLC and flat 4x4 scaling: %w", h264.ErrUnsupported)
	}
	return nil
}

func switchingMode(hdr *syntax.SliceHeader) transform.SwitchingMode {
	if hdr.Type == syntax.SliceTypeSI {
		return transform.SI
	}
	if hdr.SPForSwitchFlag {
		return transform.SPSwitching
	}
	return transform.SPPrimary
}

// reconstructSwitching 仅适配采样存储，所有系数域运算由规范 8.6 内核实现。
// SI 必须逐块预测并立即重建，后续块才能看到先前块的切换域结果。
// CBP 为零也不能跳过本过程：SP Skip 同样需要量化预测样本。
func (fs *frameState) reconstructSwitching(mbx, mby, qp, qs int, mode transform.SwitchingMode, intra bool, res *residual) error {
	pl := &fs.pic.Planes[0]
	info := &fs.info[fs.mbAddress(mbx, mby)]
	for n := range 16 {
		raster := blockRaster(n)
		bx, by := raster%4, raster/4
		x, y := mbx*16+bx*4, mby*16+by*4
		if intra {
			if err := fs.predictIntra4(mbx, mby, bx, by, x, y, int(info.intraModes[raster])); err != nil {
				return err
			}
		}
		var samples [16]uint8
		for row := range 4 {
			copy(samples[row*4:row*4+4], pl.Data[pl.Index(x, y+row):pl.Index(x, y+row)+4])
		}
		if err := transform.ReconstructSwitchingLuma4x4(&samples, &samples, &res.luma[raster], qp, qs, mode); err != nil {
			return err
		}
		for row := range 4 {
			copy(pl.Data[pl.Index(x, y+row):pl.Index(x, y+row)+4], samples[row*4:row*4+4])
		}
	}
	if intra {
		if err := fs.predictChroma(mbx, mby, int(info.chromaMode)); err != nil {
			return err
		}
	}
	for comp := range 2 {
		pl := &fs.pic.Planes[comp+1]
		x, y := mbx*8, mby*8
		var samples [64]uint8
		for row := range 8 {
			copy(samples[row*8:row*8+8], pl.Data[pl.Index(x, y+row):pl.Index(x, y+row)+8])
		}
		dc := (*[4]int32)(res.chromaDC[comp][:4])
		ac := (*[4][16]int32)(res.chroma[comp][:4])
		offset := int(fs.pps.ChromaQPIndexOffset)
		if comp == 1 {
			offset = int(fs.pps.SecondChromaQPIndexOffset)
		}
		if err := transform.ReconstructSwitchingChroma420(&samples, &samples, dc, ac, qp, qs, offset, mode); err != nil {
			return err
		}
		for row := range 8 {
			copy(pl.Data[pl.Index(x, y+row):pl.Index(x, y+row)+8], samples[row*8:row*8+8])
		}
	}
	return nil
}
